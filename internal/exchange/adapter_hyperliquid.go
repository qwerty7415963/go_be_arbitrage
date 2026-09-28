package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	hyperliquidBaseURL         = "https://api.hyperliquid.xyz"
	hyperliquidFundingInterval = 3600 // 1 hour in seconds (Hyperliquid funds hourly)
)

// HyperliquidAdapter implements REST-based funding fetching for Hyperliquid
// via the public, unauthenticated POST /info metaAndAssetCtxs endpoint,
// which returns every perp market with its current funding in one call.
type HyperliquidAdapter struct {
	code    string
	baseURL string
	client  *http.Client
}

// HyperliquidUniverseEntry is one row of the metaAndAssetCtxs universe array.
type HyperliquidUniverseEntry struct {
	Name       string `json:"name"`
	IsDelisted bool   `json:"isDelisted"`
}

// HyperliquidAssetCtx is the context row aligned by index with the universe.
type HyperliquidAssetCtx struct {
	Funding      string `json:"funding"`
	OpenInterest string `json:"openInterest"`
	MarkPx       string `json:"markPx"`
	OraclePx     string `json:"oraclePx"`
}

type HyperliquidFundingData struct {
	Symbol      string
	BaseAsset   string
	QuoteAsset  string
	FundingRate string
	MarkPrice   string
	IndexPrice  string
	OI          string
	ObservedAt  time.Time
}

func NewHyperliquidAdapter() *HyperliquidAdapter {
	return NewHyperliquidAdapterWithURL(hyperliquidBaseURL)
}

// NewHyperliquidAdapterWithURL overrides the API base (tests).
func NewHyperliquidAdapterWithURL(baseURL string) *HyperliquidAdapter {
	return &HyperliquidAdapter{
		code:    "hyperliquid",
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *HyperliquidAdapter) GetVenueCode() string {
	return a.code
}

func (a *HyperliquidAdapter) GetFundingInterval() int {
	return hyperliquidFundingInterval
}

// FetchAllFunding fetches current funding for every perp market.
func (a *HyperliquidAdapter) FetchAllFunding(ctx context.Context) ([]HyperliquidFundingData, error) {
	body, err := json.Marshal(map[string]string{"type": "metaAndAssetCtxs"})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ArbitragePlatform/1.0")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HyperliquidAPIError{Status: resp.StatusCode, Body: truncateHLBody(raw)}
	}

	var outer []json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, err
	}
	if len(outer) < 2 {
		return nil, &HyperliquidAPIError{Status: resp.StatusCode, Body: "metaAndAssetCtxs: expected [universe, ctxs]"}
	}

	var universe []HyperliquidUniverseEntry
	if err := json.Unmarshal(outer[0], &universe); err != nil {
		return nil, err
	}
	var ctxs []HyperliquidAssetCtx
	if err := json.Unmarshal(outer[1], &ctxs); err != nil {
		return nil, err
	}

	now := time.Now()
	n := len(universe)
	if len(ctxs) < n {
		n = len(ctxs) // tolerate length mismatch, FA-U-10
	}

	markets := make([]HyperliquidFundingData, 0, n)
	for i := 0; i < n; i++ {
		u := universe[i]
		if u.Name == "" || strings.HasPrefix(u.Name, "@") || u.IsDelisted {
			continue
		}
		c := ctxs[i]
		markets = append(markets, HyperliquidFundingData{
			Symbol:    u.Name,
			BaseAsset: u.Name,
			// USDT-canonical convention: matches the BTCUSDT/ETHUSDT seeds
			// (and Binance-style discovery) so HL shares instrument_ids
			// with the other venues. Settlement currency is irrelevant
			// to the funding-rate comparison.
			QuoteAsset:  "USDT",
			FundingRate: c.Funding,
			MarkPrice:   c.MarkPx,
			IndexPrice:  c.OraclePx,
			OI:          c.OpenInterest,
			ObservedAt:  now,
		})
	}
	return markets, nil
}

// HyperliquidAPIError is a non-200 / malformed envelope from /info.
type HyperliquidAPIError struct {
	Status int
	Body   string
}

func (e *HyperliquidAPIError) Error() string {
	return "hyperliquid info: status " + strconv.Itoa(e.Status) + ": " + e.Body
}

func truncateHLBody(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
