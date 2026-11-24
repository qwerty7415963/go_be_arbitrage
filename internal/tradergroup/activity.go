package tradergroup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

type cachedTransfers struct {
	at   time.Time
	page *trader.TransfersPage
	err  error
}

type activityMember struct {
	venueID   uuid.UUID
	venueCode string
	addr      string
}

// Activity implements contract §6.7.
func (r *Repository) Activity(ctx context.Context, groupID, userID uuid.UUID, f ActivityFilter) (*ActivityPage, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}

	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	f.Wallet = strings.ToLower(strings.TrimSpace(f.Wallet))
	f.Asset = strings.TrimSpace(f.Asset)
	f.Type = strings.TrimSpace(f.Type)

	// members
	mrows, err := r.pool.Query(ctx, `
		SELECT m.venue_id, v.code, m.wallet_address
		FROM trader_group_members m
		JOIN venues v ON v.id = m.venue_id
		WHERE m.group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	var members []activityMember
	venueCodeByID := map[uuid.UUID]string{}
	for mrows.Next() {
		var m activityMember
		if err := mrows.Scan(&m.venueID, &m.venueCode, &m.addr); err != nil {
			mrows.Close()
			return nil, err
		}
		m.addr = strings.ToLower(strings.TrimSpace(m.addr))
		m.venueCode = strings.TrimSpace(m.venueCode)
		members = append(members, m)
		venueCodeByID[m.venueID] = m.venueCode
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	sources := map[string]bool{
		"trades":     true,
		"fills":      true,
		"transfers":  false,
		"membership": true,
		"alerts":     true,
	}
	partial := false

	if len(members) == 0 {
		return &ActivityPage{Items: []ActivityEvent{}, NextCursor: "", HasMore: false, Partial: false, Sources: sources}, nil
	}

	transferSource := len(members) <= 10 && r.transferFetcher != nil
	sources["transfers"] = transferSource
	if len(members) > 10 {
		partial = true
	}

	events := []ActivityEvent{}

	// trader_trades (closed)
	{
		vids := make([]uuid.UUID, len(members))
		addrs := make([]string, len(members))
		for i, m := range members {
			vids[i] = m.venueID
			addrs[i] = m.addr
		}
		rows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, market, side, opened_at, closed_at, volume, pnl, fees, fills
			FROM trader_trades
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`,
			vids, addrs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var vid uuid.UUID
			var addr, market, side string
			var opened, closed time.Time
			var volume, pnl, fees float64
			var fills int
			if err := rows.Scan(&vid, &addr, &market, &side, &opened, &closed, &volume, &pnl, &fees, &fills); err != nil {
				rows.Close()
				return nil, err
			}
			addr = strings.ToLower(strings.TrimSpace(addr))
			vc := venueCodeByID[vid]
			id := hashStringToInt64(fmt.Sprintf("trade|%s|%s|%s|%s", vid.String(), addr, market, closed.UTC().Format(time.RFC3339Nano)))
			pnlPtr := pnl
			ev := ActivityEvent{
				ID:            id,
				Type:          "position_closed",
				OccurredAt:    closed.UTC(),
				Venue:         vc,
				WalletAddress: addr,
				Coin:          market,
				SizeDelta:     nil,
				Price:         nil,
				RealizedPnl:   &pnlPtr,
				Meta: map[string]any{
					"side":      side,
					"opened_at": opened.UTC(),
					"volume":    volume,
					"fees":      fees,
					"fills":     fills,
				},
			}
			events = append(events, ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// trader_wallet_event (opened/increased/reduced)
	{
		vids := make([]uuid.UUID, len(members))
		addrs := make([]string, len(members))
		for i, m := range members {
			vids[i] = m.venueID
			addrs[i] = m.addr
		}
		rows, err := r.pool.Query(ctx, `
			SELECT id, venue_id, wallet_address, coin, event_type, size_delta, price, realized_pnl, occurred_at
			FROM trader_wallet_event
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			  AND event_type IN ('position_opened','position_increased','position_reduced')`,
			vids, addrs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var vid uuid.UUID
			var addr, coin, eventType string
			var sizeDelta float64
			var price, realized *float64
			var occurred time.Time
			if err := rows.Scan(&id, &vid, &addr, &coin, &eventType, &sizeDelta, &price, &realized, &occurred); err != nil {
				rows.Close()
				return nil, err
			}
			addr = strings.ToLower(strings.TrimSpace(addr))
			vc := venueCodeByID[vid]
			sd := sizeDelta
			ev := ActivityEvent{
				ID:            id,
				Type:          eventType,
				OccurredAt:    occurred.UTC(),
				Venue:         vc,
				WalletAddress: addr,
				Coin:          coin,
				SizeDelta:     &sd,
				Price:         price,
				RealizedPnl:   realized,
			}
			events = append(events, ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// group_membership_events
	{
		rows, err := r.pool.Query(ctx, `
			SELECT e.id, e.venue_id, v.code, e.wallet_address, e.event_type, e.created_at
			FROM group_membership_events e
			JOIN venues v ON v.id = e.venue_id
			WHERE e.group_id = $1`, groupID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var vid uuid.UUID
			var vc, addr, eventType string
			var created time.Time
			if err := rows.Scan(&id, &vid, &vc, &addr, &eventType, &created); err != nil {
				rows.Close()
				return nil, err
			}
			addr = strings.ToLower(strings.TrimSpace(addr))
			ev := ActivityEvent{
				ID:            id,
				Type:          eventType,
				OccurredAt:    created.UTC(),
				Venue:         vc,
				WalletAddress: addr,
				Coin:          "",
				SizeDelta:     nil,
				Price:         nil,
				RealizedPnl:   nil,
			}
			events = append(events, ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// group_alert_events
	{
		rows, err := r.pool.Query(ctx, `
			SELECT id, type, payload, triggered_at
			FROM group_alert_events
			WHERE group_id = $1`, groupID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var typ string
			var payloadBytes []byte
			var triggered time.Time
			if err := rows.Scan(&id, &typ, &payloadBytes, &triggered); err != nil {
				rows.Close()
				return nil, err
			}
			var payload map[string]any
			if len(payloadBytes) > 0 {
				_ = json.Unmarshal(payloadBytes, &payload)
			}
			if payload == nil {
				payload = map[string]any{}
			}
			wallet := ""
			if v, ok := payload["wallet_address"].(string); ok {
				wallet = strings.ToLower(strings.TrimSpace(v))
			} else if v, ok := payload["wallet"].(string); ok {
				wallet = strings.ToLower(strings.TrimSpace(v))
			}
			coin := ""
			if v, ok := payload["coin"].(string); ok {
				coin = v
			} else if v, ok := payload["asset"].(string); ok {
				coin = v
			}
			venue := ""
			if v, ok := payload["venue"].(string); ok {
				venue = v
			}
			ev := ActivityEvent{
				ID:            hashStringToInt64("alert|" + id.String()),
				Type:          typ,
				OccurredAt:    triggered.UTC(),
				Venue:         venue,
				WalletAddress: wallet,
				Coin:          coin,
				SizeDelta:     nil,
				Price:         nil,
				RealizedPnl:   nil,
				Meta:          payload,
			}
			events = append(events, ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// transfers (if <=10 members)
	if transferSource {
		days := 30
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		results := make([][]ActivityEvent, len(members))
		errs := make([]error, len(members))
		for i, m := range members {
			wg.Add(1)
			go func(i int, m activityMember) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				key := m.venueCode + "|" + m.addr + "|" + strconv.Itoa(days)
				// cache check
				if v, ok := r.transferCache.Load(key); ok {
					if ce, ok2 := v.(cachedTransfers); ok2 && time.Since(ce.at) < 60*time.Second {
						if ce.err != nil {
							errs[i] = ce.err
							return
						}
						results[i] = transfersToEvents(m, ce.page)
						return
					}
				}
				page, err := r.transferFetcher(ctx, m.venueCode, m.addr, days)
				r.transferCache.Store(key, cachedTransfers{at: time.Now(), page: page, err: err})
				if err != nil {
					errs[i] = err
					return
				}
				results[i] = transfersToEvents(m, page)
			}(i, m)
		}
		wg.Wait()
		for _, evs := range results {
			events = append(events, evs...)
		}
		for _, e := range errs {
			if e != nil {
				partial = true
			}
		}
	}

	// filter in Go
	filtered := []ActivityEvent{}
	for _, ev := range events {
		if f.Wallet != "" && !strings.EqualFold(ev.WalletAddress, f.Wallet) {
			continue
		}
		if f.Asset != "" && !strings.EqualFold(ev.Coin, f.Asset) {
			continue
		}
		if f.Type != "" && !strings.EqualFold(ev.Type, f.Type) {
			continue
		}
		if f.From != nil && ev.OccurredAt.Before(*f.From) {
			continue
		}
		if f.To != nil && ev.OccurredAt.After(*f.To) {
			continue
		}
		filtered = append(filtered, ev)
	}

	// sort occurred_at desc, id desc
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].OccurredAt.Equal(filtered[j].OccurredAt) {
			return filtered[i].OccurredAt.After(filtered[j].OccurredAt)
		}
		return filtered[i].ID > filtered[j].ID
	})

	// cursor
	if strings.TrimSpace(f.Cursor) != "" {
		occ, id, err := decodeActivityCursor(r.cursorSecret, f.Cursor)
		if err != nil {
			return nil, err
		}
		out := []ActivityEvent{}
		for _, ev := range filtered {
			if ev.OccurredAt.Before(occ) || (ev.OccurredAt.Equal(occ) && ev.ID < id) {
				out = append(out, ev)
			}
		}
		filtered = out
	}

	hasMore := len(filtered) > f.Limit
	if hasMore {
		filtered = filtered[:f.Limit]
	}
	if filtered == nil {
		filtered = []ActivityEvent{}
	}

	page := &ActivityPage{
		Items:   filtered,
		HasMore: hasMore,
		Partial: partial,
		Sources: sources,
	}
	if hasMore && len(filtered) > 0 {
		last := filtered[len(filtered)-1]
		cur, err := encodeActivityCursor(r.cursorSecret, last.OccurredAt, last.ID)
		if err != nil {
			return nil, err
		}
		page.NextCursor = cur
	}
	return page, nil
}

func transfersToEvents(m activityMember, page *trader.TransfersPage) []ActivityEvent {
	out := []ActivityEvent{}
	if page == nil {
		return out
	}
	for _, tr := range page.Rows {
		sizeDelta := tr.Amount
		if sizeDelta == nil {
			sizeDelta = tr.UsdcValue
		}
		coin := ""
		if tr.Token != nil {
			coin = *tr.Token
		}
		meta := map[string]any{
			"hash":            tr.Hash,
			"usdc":            tr.Usdc,
			"amount":          tr.Amount,
			"usdc_value":      tr.UsdcValue,
			"is_deposit":      tr.IsDeposit,
			"source_dex":      tr.SourceDex,
			"destination_dex": tr.DestinationDex,
			"counterparty":    tr.Counterparty,
		}
		ev := ActivityEvent{
			ID:            hashStringToInt64("transfer|" + m.venueCode + "|" + m.addr + "|" + tr.Hash + "|" + tr.Time.UTC().Format(time.RFC3339Nano)),
			Type:          tr.Type,
			OccurredAt:    tr.Time.UTC(),
			Venue:         m.venueCode,
			WalletAddress: m.addr,
			Coin:          coin,
			SizeDelta:     sizeDelta,
			Price:         nil,
			RealizedPnl:   nil,
			Meta:          meta,
		}
		out = append(out, ev)
	}
	return out
}

func hashStringToInt64(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return int64(h.Sum64())
}

func encodeActivityCursor(secret []byte, occurredAt time.Time, id int64) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"o": occurredAt.UTC().Format(time.RFC3339Nano),
		"i": id,
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeActivityCursor(secret []byte, raw string) (time.Time, int64, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid cursor signature")
	}
	var c struct {
		O string `json:"o"`
		I int64  `json:"i"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	occ, err := time.Parse(time.RFC3339Nano, c.O)
	if err != nil {
		return time.Time{}, 0, domain.NewError(domain.ErrCodeInvalidFilter, "malformed cursor")
	}
	return occ.UTC(), c.I, nil
}
