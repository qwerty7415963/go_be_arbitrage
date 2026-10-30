package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

type DispatchMeta struct {
	UserID uuid.UUID
	ChatID int64
	Locale telegram.Locale
	Group  string
	Who    string
	Link   string
	Wallet string
}

type Dispatcher struct {
	repo         *Repository
	coalescer    *Coalescer
	appPublicURL string
	events       chan trader.WalletEvent
	mu           sync.Mutex
}

func NewDispatcher(args ...interface{}) *Dispatcher {
	d := &Dispatcher{
		coalescer:    NewCoalescer(3 * time.Second),
		appPublicURL: "http://localhost:3000",
		events:       make(chan trader.WalletEvent, 1024),
	}
	for _, a := range args {
		switch v := a.(type) {
		case *Repository:
			d.repo = v
		case Repository:
			d.repo = &v
		case string:
			if d.appPublicURL == "http://localhost:3000" {
				d.appPublicURL = v
			}
		case time.Duration:
			if v > 0 {
				d.coalescer = NewCoalescer(v)
			}
		case *telegram.Service:
			// no-op; service used for config only
		}
	}
	return d
}

func (d *Dispatcher) Enqueue(ev trader.WalletEvent) bool {
	if d == nil {
		return false
	}
	select {
	case d.events <- ev:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// flush remaining
			_ = d.Flush(ctx)
			return
		case ev := <-d.events:
			_ = d.HandleEvent(ctx, ev)
		case <-ticker.C:
			for _, ci := range d.coalescer.FlushExpired(time.Now()) {
				_ = d.dispatchCoalesced(ctx, ci)
			}
		}
	}
}

func (d *Dispatcher) HandleEvent(ctx context.Context, ev trader.WalletEvent) error {
	if d == nil {
		return nil
	}
	intent, ok := MapWalletEvent(ev)
	if !ok {
		return nil
	}
	d.coalescer.Add(intent)
	for _, ci := range d.coalescer.FlushExpired(time.Now()) {
		_ = d.dispatchCoalesced(ctx, ci)
	}
	return nil
}

func (d *Dispatcher) Flush(ctx context.Context) error {
	if d == nil {
		return nil
	}
	for _, ci := range d.coalescer.Flush() {
		_ = d.dispatchCoalesced(ctx, ci)
	}
	return nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, intent Intent, meta DispatchMeta) error {
	if d == nil {
		return nil
	}
	if meta.UserID != uuid.Nil {
		if intent.UserID == uuid.Nil {
			intent.UserID = meta.UserID
		}
		intent.DedupKey = perUserDedupKey(meta.UserID, intent.DedupKey)
	}
	if intent.DedupKey != "" && d.repo != nil {
		exists, err := d.repo.HasDedupKey(ctx, intent.DedupKey)
		if err == nil && exists {
			return nil
		}
	}
	wallet := intent.Wallet
	if meta.Wallet != "" {
		wallet = meta.Wallet
	}
	if wallet == "" {
		wallet = stringsLower(intent.Wallet)
	}
	locale := meta.Locale
	if !locale.IsValid() {
		locale = telegram.LocaleVI
	}
	group := meta.Group
	who := meta.Who
	if who == "" {
		who = wallet
	}
	link := meta.Link
	if link == "" && d.appPublicURL != "" {
		link = fmt.Sprintf("%s/%s/traders/%s", d.appPublicURL, string(locale), wallet)
	}
	data := map[string]any{}
	for k, v := range intent.Data {
		data[k] = v
	}
	data["group"] = group
	data["who"] = who
	data["link"] = link

	body := telegram.RenderMessage(intent.Kind, locale, data)

	row := &OutboxRow{
		UserID:   meta.UserID,
		ChatID:   meta.ChatID,
		DedupKey: nil,
		Body:     body,
		Attempts: 0,
	}
	if intent.DedupKey != "" {
		k := intent.DedupKey
		row.DedupKey = &k
	}
	if d.repo != nil {
		_, err := d.repo.InsertOutbox(ctx, row)
		return err
	}
	return nil
}

func (d *Dispatcher) dispatchCoalesced(ctx context.Context, intent Intent) error {
	if d == nil || d.repo == nil {
		return nil
	}
	targets, err := d.repo.TargetsForWallet(ctx, intent.Wallet)
	if err != nil {
		return err
	}
	for _, t := range targets {
		meta := DispatchMeta{
			UserID: t.UserID,
			ChatID: t.ChatID,
			Locale: t.Locale,
			Group:  t.GroupName,
			Who:    t.Who,
			Wallet: t.Wallet,
		}
		_ = d.Dispatch(ctx, intent, meta)
	}
	return nil
}

func perUserDedupKey(userID uuid.UUID, key string) string {
	if strings.TrimSpace(key) == "" {
		return key
	}
	if strings.HasPrefix(key, "user:") {
		rest := key[len("user:"):]
		if idx := strings.Index(rest, ":"); idx >= 0 {
			rest = rest[idx+1:]
			return fmt.Sprintf("user:%s:%s", userID, rest)
		}
	}
	return fmt.Sprintf("user:%s:%s", userID, key)
}

func stringsLower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 32
		}
	}
	return string(out)
}
