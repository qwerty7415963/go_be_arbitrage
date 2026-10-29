package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DecimalString accepts a JSON string or number and stores the canonical
// string form. Live clearinghouseState mixes both: leverage.value and
// maxLeverage arrive as numbers (e.g. 20, 50) while prices/PnL arrive as
// decimal strings (e.g. "2986.3").
type DecimalString string

func (d *DecimalString) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || string(raw) == "null" {
		*d = ""
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*d = DecimalString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return fmt.Errorf("decimal: bad number %s", string(raw))
	}
	*d = DecimalString(n.String())
	return nil
}

func (d DecimalString) String() string { return string(d) }

// Float parses the decimal string (empty => 0 + error for callers to skip).
func (d DecimalString) Float() (float64, error) {
	s := string(d)
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}

// AssetPosition is one open position from clearinghouseState.
type AssetPosition struct {
	Position struct {
		Coin           string        `json:"coin"`
		Szi            DecimalString `json:"szi"`
		EntryPx        DecimalString `json:"entryPx"`
		PositionValue  DecimalString `json:"positionValue"`
		UnrealizedPnl  DecimalString `json:"unrealizedPnl"`
		ReturnOnEquity DecimalString `json:"returnOnEquity"`
		LiquidationPx  DecimalString `json:"liquidationPx"`
		MarginUsed     DecimalString `json:"marginUsed"`
		MaxLeverage    DecimalString `json:"maxLeverage"`
		Leverage       struct {
			Type  string        `json:"type"`
			Value DecimalString `json:"value"`
		} `json:"leverage"`
	} `json:"position"`
	Type string `json:"type"`
}

// ClearinghouseState is the perp account summary for one user.
// Withdrawable + CrossMarginSummary observed live 2026-10-07 (contract
// WALLET-TABS v1 §0); absent upstream values decode as "" (nil downstream).
type ClearinghouseState struct {
	MarginSummary struct {
		AccountValue    DecimalString `json:"accountValue"`
		TotalNtlPos     DecimalString `json:"totalNtlPos"`
		TotalMarginUsed DecimalString `json:"totalMarginUsed"`
	} `json:"marginSummary"`
	CrossMarginSummary struct {
		AccountValue    DecimalString `json:"accountValue"`
		TotalNtlPos     DecimalString `json:"totalNtlPos"`
		TotalMarginUsed DecimalString `json:"totalMarginUsed"`
	} `json:"crossMarginSummary"`
	Withdrawable   DecimalString   `json:"withdrawable"`
	AssetPositions []AssetPosition `json:"assetPositions"`
	Time           int64           `json:"time"`
}

type clearinghouseRequest struct {
	Type string `json:"type"`
	User string `json:"user"`
	Dex  string `json:"dex,omitempty"`
}

// FetchClearinghouseState returns the perp account summary for address.
// Public endpoint (no auth): POST {type:"clearinghouseState", user} to /info.
// When dex is non-empty, the request carries "dex":"<name>" (HIP-3 multi-dex).
// Paced via the shared adaptive pacer like FetchWindow.
func (c *Client) FetchClearinghouseState(ctx context.Context, address string, dex ...string) (*ClearinghouseState, error) {
	if err := c.pacer.Wait(ctx); err != nil {
		return nil, err
	}
	cr := clearinghouseRequest{Type: "clearinghouseState", User: address}
	if len(dex) > 0 {
		cr.Dex = strings.TrimSpace(dex[0])
	}
	body, err := json.Marshal(cr)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid clearinghouse: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid clearinghouse read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return nil, fmt.Errorf("hyperliquid clearinghouse: status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	c.noteStatus(resp.StatusCode)

	var state ClearinghouseState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("hyperliquid clearinghouse decode: %w", err)
	}
	if state.AssetPositions == nil {
		state.AssetPositions = []AssetPosition{}
	}
	return &state, nil
}

// PerpDexEntry is one element of the perpDexs response (index 0 is null).
type PerpDexEntry struct {
	Name string `json:"name"`
}

// FetchPerpDexs returns the names of the named perp dexes (HIP-3). The
// default dex (array index 0, null) is implicit and never included. Null or
// malformed entries are skipped. The returned slice is never nil.
func (c *Client) FetchPerpDexs(ctx context.Context) ([]string, error) {
	if err := c.pacer.Wait(ctx); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]string{"type": "perpDexs"})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid perpDexs: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid perpDexs read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return nil, fmt.Errorf("hyperliquid perpDexs: status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	c.noteStatus(resp.StatusCode)

	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("hyperliquid perpDexs decode: %w", err)
	}

	out := []string{}
	for _, e := range entries {
		trimmed := bytes.TrimSpace(e)
		if len(trimmed) == 0 || string(trimmed) == "null" {
			continue
		}
		var entry PerpDexEntry
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
