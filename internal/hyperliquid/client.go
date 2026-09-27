package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
)

const (
	// DefaultBaseURL is the Hyperliquid mainnet public API.
	DefaultBaseURL = "https://api.hyperliquid.xyz"
	// TestnetBaseURL is the Hyperliquid testnet public API.
	TestnetBaseURL = "https://api.hyperliquid-testnet.xyz"

	// maxFillsPerResponse is the documented per-response cap for
	// userFillsByTime. A full page means the window must be subdivided.
	maxFillsPerResponse = 2000

	// minSliceMs is the subdivision floor: a full page at this slice means
	// the venue cannot serve the window granularity — the result is flagged
	// truncated (ING-I-03). 1h keeps backfill call counts sane.
	minSliceMs = int64(3_600_000)
)

// VenueCode is the venues.code row seeded by migration 000015.
const VenueCode = "hyperliquid"

// Compile-time guarantee: *Client satisfies the venue-agnostic ingestion
// seam, so BackfillService never depends on Hyperliquid shapes.
var _ wallet.FillFetcher = (*Client)(nil)

// Fill is one row of the public userFillsByTime response. Numeric fields
// arrive as decimal strings; unknown fields are ignored by encoding/json.
type Fill struct {
	Coin          string `json:"coin"`
	Px            string `json:"px"`
	Sz            string `json:"sz"`
	Side          string `json:"side"` // B (bid/buy) | A (ask/sell)
	Time          int64  `json:"time"` // ms since epoch
	StartPosition string `json:"startPosition"`
	Dir           string `json:"dir"` // e.g. "Open Long", "Close Short"
	ClosedPnl     string `json:"closedPnl"`
	Hash          string `json:"hash"`
	Oid           int64  `json:"oid"`
	Tid           int64  `json:"tid"`
	Crossed       bool   `json:"crossed"`
	Fee           string `json:"fee"`
	FeeToken      string `json:"feeToken"`
}

// Client queries the public Hyperliquid info API (no auth). Calls are paced
// by minInterval to respect the venue weight limits.
type Client struct {
	baseURL     string
	http        *http.Client
	minInterval time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewClient(baseURL string, timeout, minInterval time.Duration) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:     baseURL,
		http:        &http.Client{Timeout: timeout},
		minInterval: minInterval,
	}
}

type fillsByTimeRequest struct {
	Type            string `json:"type"`
	User            string `json:"user"`
	StartTime       int64  `json:"startTime"`
	EndTime         int64  `json:"endTime"`
	AggregateByTime bool   `json:"aggregateByTime"`
}

// FetchWindow performs one userFillsByTime call for [startMs, endMs]
// (inclusive). Rows arrive newest-first.
func (c *Client) FetchWindow(ctx context.Context, address string, startMs, endMs int64) ([]Fill, error) {
	c.pace()

	body, err := json.Marshal(fillsByTimeRequest{
		Type:            "userFillsByTime",
		User:            address,
		StartTime:       startMs,
		EndTime:         endMs,
		AggregateByTime: true, // combine partial fills of one crossing order
	})
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
		return nil, fmt.Errorf("hyperliquid info: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid info: status %d: %s", resp.StatusCode, truncate(raw, 300))
	}

	var fills []Fill
	if err := json.Unmarshal(raw, &fills); err != nil {
		return nil, fmt.Errorf("hyperliquid decode: %w", err)
	}
	if fills == nil {
		fills = []Fill{}
	}
	return fills, nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

func (c *Client) pace() {
	if c.minInterval <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.minInterval - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

// FetchAll returns every fill in [startMs, endMs] oldest-first, subdividing
// full pages recursively. truncated=true when a min-slice page is still full
// (the venue's 10k most-recent cap likely hides older fills — ING-I-03).
// Rows are deduplicated by (coin, tid) across overlapping slice boundaries.
func (c *Client) FetchAll(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error) {
	var out []Fill
	truncated := false

	var walk func(s, e int64) error
	walk = func(s, e int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fills, err := c.FetchWindow(ctx, address, s, e)
		if err != nil {
			return err
		}
		if len(fills) < maxFillsPerResponse {
			out = append(out, fills...)
			return nil
		}
		if e-s <= minSliceMs {
			out = append(out, fills...)
			truncated = true
			return nil
		}
		mid := s + (e-s)/2
		if err := walk(s, mid); err != nil {
			return err
		}
		return walk(mid, e)
	}

	if err := walk(startMs, endMs); err != nil {
		return nil, false, err
	}

	// Deduplicate (inclusive slice boundaries can overlap on one ms) and
	// order oldest-first for the engine.
	seen := make(map[string]bool, len(out))
	deduped := make([]Fill, 0, len(out))
	for _, f := range out {
		key := f.Coin + ":" + itoa(f.Tid)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, f)
	}
	sortFillsAsc(deduped)
	return deduped, truncated, nil
}

func itoa(v int64) string {
	return fmt.Sprintf("%d", v)
}

func sortFillsAsc(fills []Fill) {
	for i := 1; i < len(fills); i++ {
		for j := i; j > 0 && lessFill(fills[j], fills[j-1]); j-- {
			fills[j], fills[j-1] = fills[j-1], fills[j]
		}
	}
}

func lessFill(a, b Fill) bool {
	if a.Time != b.Time {
		return a.Time < b.Time
	}
	return a.Tid < b.Tid
}

// FetchFills implements wallet.FillFetcher: paginated fetch plus
// normalization to engine-ready inputs.
func (c *Client) FetchFills(ctx context.Context, address string, startMs, endMs int64) ([]wallet.FillInput, bool, error) {
	raw, truncated, err := c.FetchAll(ctx, address, startMs, endMs)
	if err != nil {
		return nil, false, err
	}
	inputs, skipped := NormalizeFills(raw)
	if skipped > 0 {
		log.Printf("hyperliquid fetch %s: skipped %d unparseable fills", address, skipped)
	}
	return inputs, truncated, nil
}
