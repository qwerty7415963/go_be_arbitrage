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
	"time"
)

// StatsBaseURL is the public Hyperliquid stats API powering the leaderboard
// (same data as app.hyperliquid.xyz/leaderboard; no auth). Pinned by the
// Phase-0 contract spike; shape changes must break the adapter loudly (BE-031)
// instead of silently ingesting garbage.
const StatsBaseURL = "https://stats-data.hyperliquid.xyz"

// maxLeaderboardBytes caps the stats payload (DISC-U-04: ~39MB observed;
// 558 headroom without risking OOM on a runaway response).
const maxLeaderboardBytes = 128 << 20

// LeaderboardWindow is one window's aggregates. All numbers arrive as decimal
// strings; ROI is a fraction (×100 for percent at use).
type LeaderboardWindow struct {
	PnL    *float64
	ROI    *float64
	Volume *float64
}

// LeaderboardRow is one trader. DisplayName is nil when anonymous.
type LeaderboardRow struct {
	Address      string
	AccountValue *float64
	DisplayName  *string
	Windows      map[string]LeaderboardWindow // day|week|month|allTime
}

func parseNum(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

// PortfolioPoint is one account-value sample (ms epoch + decimal string upstream).
type PortfolioPoint struct {
	Time  int64
	Value string
}

// FetchPortfolio returns account-value histories per window (day/week/month/
// allTime plus perp* variants; callers pick the base four). Single call,
// no pagination.
func (c *Client) FetchPortfolio(ctx context.Context, address string) (map[string][]PortfolioPoint, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"type": "portfolio", "user": address})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid portfolio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid portfolio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return nil, fmt.Errorf("hyperliquid portfolio: status %d", resp.StatusCode)
	}
	c.noteStatus(resp.StatusCode)
	var doc [][2]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("hyperliquid portfolio decode: %w", err)
	}
	out := map[string][]PortfolioPoint{}
	for _, entry := range doc {
		var name string
		if err := json.Unmarshal(entry[0], &name); err != nil {
			continue
		}
		var payload struct {
			History [][2]json.RawMessage `json:"accountValueHistory"`
		}
		if err := json.Unmarshal(entry[1], &payload); err != nil {
			continue
		}
		for _, p := range payload.History {
			var ms int64
			var val string
			if json.Unmarshal(p[0], &ms) != nil || json.Unmarshal(p[1], &val) != nil {
				continue
			}
			out[name] = append(out[name], PortfolioPoint{Time: ms, Value: val})
		}
	}
	return out, nil
}

// FetchLeaderboard downloads the full board (single dump, no pagination).
// Top-level shape violations are hard errors (BE-031: fail safe, keep old
// data); per-row problems are reported as skips by the caller via the
// returned rows (malformed addresses are filtered here).
func (c *Client) FetchLeaderboard(ctx context.Context) ([]LeaderboardRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.statsBaseURL+"/Mainnet/leaderboard", nil)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid leaderboard request: %w", err)
	}
	req.Header.Set("User-Agent", "arbitrage-be/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid leaderboard: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.noteStatus(resp.StatusCode)
		return nil, fmt.Errorf("hyperliquid leaderboard: status %d", resp.StatusCode)
	}
	c.noteStatus(resp.StatusCode)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxLeaderboardBytes))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid leaderboard read: %w", err)
	}

	var doc struct {
		Rows []struct {
			EthAddress         string            `json:"ethAddress"`
			AccountValue       string            `json:"accountValue"`
			DisplayName        *string           `json:"displayName"`
			WindowPerformances []json.RawMessage `json:"windowPerformances"`
		} `json:"leaderboardRows"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("hyperliquid leaderboard decode: %w", err)
	}
	if doc.Rows == nil {
		return nil, fmt.Errorf("hyperliquid leaderboard: missing leaderboardRows (schema drift?)")
	}

	out := make([]LeaderboardRow, 0, len(doc.Rows))
	for _, r := range doc.Rows {
		addr := strings.ToLower(strings.TrimSpace(r.EthAddress))
		if len(addr) != 42 || !strings.HasPrefix(addr, "0x") {
			continue // malformed address: skip safely (BE-006); counted by caller via len delta
		}
		row := LeaderboardRow{
			Address:      addr,
			AccountValue: parseNum(r.AccountValue),
			DisplayName:  r.DisplayName,
			Windows:      map[string]LeaderboardWindow{},
		}
		for _, entry := range r.WindowPerformances {
			var wp [2]json.RawMessage
			if err := json.Unmarshal(entry, &wp); err != nil {
				continue // malformed window entry: skip entry, keep row
			}
			var perf struct {
				PnL string `json:"pnl"`
				ROI string `json:"roi"`
				Vlm string `json:"vlm"`
			}
			var name string
			if err := json.Unmarshal(wp[0], &name); err != nil {
				continue
			}
			if json.Unmarshal(wp[1], &perf) != nil {
				continue
			}
			row.Windows[name] = LeaderboardWindow{
				PnL: parseNum(perf.PnL), ROI: parseNum(perf.ROI), Volume: parseNum(perf.Vlm),
			}
		}
		out = append(out, row)
	}
	return out, nil
}
