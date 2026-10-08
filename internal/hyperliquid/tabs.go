package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Wallet-tabs on-demand sources (contract WALLET-TABS v1 §0, probe live
// 2026-10-07 mainnet POST api.hyperliquid.xyz/info). All methods pace via the
// shared AdaptivePacer like FetchWindow/FetchClearinghouseState.

// SpotBalance is one row of spotClearinghouseState.balances.
type SpotBalance struct {
	Coin     string `json:"coin"`
	Token    string `json:"token"`
	Total    string `json:"total"`
	Hold     string `json:"hold"`
	EntryNtl string `json:"entryNtl"`
}

// SpotState is the spot account summary for one user.
type SpotState struct {
	Balances []SpotBalance `json:"balances"`
}

// LedgerUpdate is one row of userNonFundingLedgerUpdates.
type LedgerUpdate struct {
	Time  int64           `json:"time"`
	Hash  string          `json:"hash"`
	Delta json.RawMessage `json:"delta"`
}

// OpenOrder is one row of openOrders.
type OpenOrder struct {
	Coin             string `json:"coin"`
	LimitPx          string `json:"limitPx"`
	Oid              int64  `json:"oid"`
	Side             string `json:"side"`
	Sz               string `json:"sz"`
	Timestamp        int64  `json:"timestamp"`
	OrderType        string `json:"orderType"`
	OrigSz           string `json:"origSz"`
	ReduceOnly       bool   `json:"reduceOnly"`
	TriggerCondition string `json:"triggerCondition"`
	TriggerPx        string `json:"triggerPx"`
	IsPositionTpsl   *bool  `json:"isPositionTpsl"`
}

// HistoricalOrder is one row of historicalOrders.
type HistoricalOrder struct {
	Order           OpenOrder `json:"order"`
	Status          string    `json:"status"`
	StatusTimestamp int64     `json:"statusTimestamp"`
}

func (c *Client) postInfo(ctx context.Context, payload any, out any, op string, maxBytes int64) error {
	if err := c.pacer.Wait(ctx); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hyperliquid %s: %w", op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return fmt.Errorf("hyperliquid %s read: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return fmt.Errorf("hyperliquid %s: status %d: %s", op, resp.StatusCode, truncate(raw, 300))
	}
	c.noteStatus(resp.StatusCode)
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("hyperliquid %s decode: %w", op, err)
	}
	return nil
}

// FetchSpotState returns the spot balances for address.
// Public: POST {type:"spotClearinghouseState", user} to /info.
func (c *Client) FetchSpotState(ctx context.Context, address string) (*SpotState, error) {
	var state SpotState
	if err := c.postInfo(ctx, map[string]string{"type": "spotClearinghouseState", "user": address}, &state, "spot", 8<<20); err != nil {
		return nil, err
	}
	if state.Balances == nil {
		state.Balances = []SpotBalance{}
	}
	return &state, nil
}

// FetchLedgerUpdates returns non-funding ledger updates since startTimeMs.
// Public: POST {type:"userNonFundingLedgerUpdates", user, startTime}.
// Funding excluded by decision (contract §0: userFunding NOT used).
func (c *Client) FetchLedgerUpdates(ctx context.Context, address string, startTimeMs int64) ([]LedgerUpdate, error) {
	var out []LedgerUpdate
	if err := c.postInfo(ctx, map[string]any{"type": "userNonFundingLedgerUpdates", "user": address, "startTime": startTimeMs}, &out, "ledger", 32<<20); err != nil {
		return nil, err
	}
	if out == nil {
		out = []LedgerUpdate{}
	}
	return out, nil
}

// FetchOpenOrders returns live open orders for address.
// Public: POST {type:"openOrders", user} to /info.
func (c *Client) FetchOpenOrders(ctx context.Context, address string) ([]OpenOrder, error) {
	var out []OpenOrder
	if err := c.postInfo(ctx, map[string]string{"type": "openOrders", "user": address}, &out, "openOrders", 8<<20); err != nil {
		return nil, err
	}
	if out == nil {
		out = []OpenOrder{}
	}
	return out, nil
}

// FetchHistoricalOrders returns the 2000 most-recent historical orders.
// Public: POST {type:"historicalOrders", user} to /info.
func (c *Client) FetchHistoricalOrders(ctx context.Context, address string) ([]HistoricalOrder, error) {
	var out []HistoricalOrder
	if err := c.postInfo(ctx, map[string]string{"type": "historicalOrders", "user": address}, &out, "historicalOrders", 16<<20); err != nil {
		return nil, err
	}
	if out == nil {
		out = []HistoricalOrder{}
	}
	return out, nil
}

// FetchUserFills returns the 2000 most-recent fills for address.
// Public: POST {type:"userFills", user} to /info. Same fill shape as
// userFillsByTime (see Fill); unknown fields ignored.
func (c *Client) FetchUserFills(ctx context.Context, address string) ([]Fill, error) {
	var out []Fill
	if err := c.postInfo(ctx, map[string]string{"type": "userFills", "user": address}, &out, "userFills", 32<<20); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Fill{}
	}
	return out, nil
}
