package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

type Bot struct {
	Token   string
	BaseURL string
	Repo    *Repository
	Service *Service
	Linker  LinkService
	Sender  *Sender
	Client  *http.Client
	Offset  int64
}

type LinkService interface {
	CreateLink(ctx context.Context, userID uuid.UUID, locale interface{}) (*LinkResponse, error)
	Status(ctx context.Context, userID uuid.UUID) (*StatusResponse, error)
	Unlink(ctx context.Context, userID uuid.UUID) error
}

// ensure Service implements LinkService
var _ LinkService = (*Service)(nil)

type tgUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

func NewBot(args ...interface{}) *Bot {
	b := &Bot{BaseURL: "https://api.telegram.org", Client: &http.Client{Timeout: 35 * time.Second}}
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if b.Token == "" {
				b.Token = v
			} else {
				b.BaseURL = v
			}
		case *Repository:
			b.Repo = v
		case *Service:
			b.Service = v
			b.Linker = v
		case LinkService:
			b.Linker = v
		case *Sender:
			b.Sender = v
		case *http.Client:
			b.Client = v
		case *config.TelegramConfig:
			if v.BotToken != "" {
				b.Token = v.BotToken
			}
			if v.BotUsername != "" {
				// store not needed
			}
		}
	}
	if b.Sender == nil && b.Token != "" {
		b.Sender = NewSender(b.Token)
	}
	if b.Linker == nil && b.Service != nil {
		b.Linker = b.Service
	}
	return b
}

func (b *Bot) Start(ctx context.Context) {
	if b == nil || b.Token == "" {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= b.Offset {
				b.Offset = u.UpdateID + 1
			}
			b.handleUpdate(ctx, u)
		}
	}
}

func (b *Bot) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	if b.Client == nil {
		b.Client = &http.Client{Timeout: 35 * time.Second}
	}
	if b.BaseURL == "" {
		b.BaseURL = "https://api.telegram.org"
	}
	u := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=30", b.BaseURL, b.Token, b.Offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if !payload.OK {
		return nil, fmt.Errorf("getUpdates failed")
	}
	return payload.Result, nil
}

func (b *Bot) handleUpdate(ctx context.Context, u tgUpdate) {
	if u.Message == nil {
		return
	}
	m := u.Message
	text := strings.TrimSpace(m.Text)
	if !strings.HasPrefix(text, "/") {
		return
	}
	fields := strings.Fields(text)
	cmd := fields[0]
	// strip bot suffix
	if idx := strings.Index(cmd, "@"); idx >= 0 {
		cmd = cmd[:idx]
	}
	switch cmd {
	case "/start":
		code := ""
		if len(fields) > 1 {
			code = fields[1]
		}
		b.handleStart(ctx, m, code)
	case "/status":
		b.handleStatus(ctx, m)
	case "/stop":
		b.handleStop(ctx, m)
	}
}

func (b *Bot) handleStart(ctx context.Context, m *tgMessage, code string) {
	if b.Repo == nil {
		return
	}
	if code == "" {
		b.reply(ctx, m.Chat.ID, "invalid code")
		return
	}
	lc, err := b.Repo.ConsumeCode(ctx, code)
	if err != nil {
		b.reply(ctx, m.Chat.ID, "Mã không hợp lệ hoặc đã hết hạn (10 phút).")
		return
	}
	// already connected?
	if existing, err := b.Repo.GetBinding(ctx, lc.UserID); err == nil && existing != nil {
		b.reply(ctx, m.Chat.ID, "Tài khoản này đã kết nối trước đó.")
		return
	}
	username := (*string)(nil)
	firstName := (*string)(nil)
	if m.From != nil {
		if m.From.Username != "" {
			u := m.From.Username
			username = &u
		}
		if m.From.FirstName != "" {
			f := m.From.FirstName
			firstName = &f
		}
	}
	binding := &Binding{
		UserID:      lc.UserID,
		ChatID:      m.Chat.ID,
		TGUsername:  username,
		TGFirstName: firstName,
		Locale:      lc.Locale,
		Status:      BindingStatusActive,
	}
	if err := b.Repo.UpsertBinding(ctx, binding); err != nil {
		b.reply(ctx, m.Chat.ID, "Lỗi kết nối.")
		return
	}
	email, _ := b.Repo.GetUserEmail(ctx, lc.UserID)
	n, _ := b.Repo.CountEnabledGroups(ctx, lc.UserID)
	msg := fmt.Sprintf("Đã kết nối với %s. Bạn sẽ nhận thông báo từ %d nhóm đang bật.", email, n)
	b.reply(ctx, m.Chat.ID, msg)
}

func (b *Bot) handleStatus(ctx context.Context, m *tgMessage) {
	if b.Repo == nil {
		return
	}
	binding, err := b.Repo.GetBindingByChatID(ctx, m.Chat.ID)
	if err != nil || binding == nil {
		b.reply(ctx, m.Chat.ID, "Chưa kết nối. Gửi /start <code> để kết nối.")
		return
	}
	n, _ := b.Repo.CountEnabledGroups(ctx, binding.UserID)
	wm, _ := b.Repo.CountWatchedWallets(ctx, binding.UserID)
	b.reply(ctx, m.Chat.ID, fmt.Sprintf("Đang theo dõi %d nhóm và %d ví.", n, wm))
}

func (b *Bot) handleStop(ctx context.Context, m *tgMessage) {
	if b.Repo == nil {
		return
	}
	binding, err := b.Repo.GetBindingByChatID(ctx, m.Chat.ID)
	if err != nil || binding == nil {
		b.reply(ctx, m.Chat.ID, "Chưa kết nối.")
		return
	}
	_ = b.Repo.DeleteBinding(ctx, binding.UserID)
	b.reply(ctx, m.Chat.ID, "Đã hủy kết nối. Bạn sẽ không nhận thông báo nữa.")
}

func (b *Bot) reply(ctx context.Context, chatID int64, html string) {
	if b.Sender == nil {
		return
	}
	if err := b.Sender.SendMessage(ctx, chatID, html); err != nil {
		if IsPermanent(err) {
			_ = b.Repo.MarkBlocked(ctx, chatID)
		}
	}
}
