package trader

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// OnDemandClient is the venue on-demand seam for the wallet tabs (contract
// WALLET-TABS v1 §1.3-§1.6). *hyperliquid.Client implements it; tests mock it.
type OnDemandClient interface {
	FetchClearinghouseState(ctx context.Context, address string) (*hyperliquid.ClearinghouseState, error)
	FetchSpotState(ctx context.Context, address string) (*hyperliquid.SpotState, error)
	FetchUserFills(ctx context.Context, address string) ([]hyperliquid.Fill, error)
	FetchOpenOrders(ctx context.Context, address string) ([]hyperliquid.OpenOrder, error)
	FetchHistoricalOrders(ctx context.Context, address string) ([]hyperliquid.HistoricalOrder, error)
	FetchLedgerUpdates(ctx context.Context, address string, startTimeMs int64) ([]hyperliquid.LedgerUpdate, error)
}

var _ OnDemandClient = (*hyperliquid.Client)(nil)

// OnDemandCache is a tiny in-memory TTL cache keyed by (type,wallet,params)
// (contract §4 shared). Default TTL 15s. Expired entries are kept for graceful
// degradation: on HL error the last value is returned with an error
// data_status instead of an empty failure.
type OnDemandCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cacheItem
}

type cacheItem struct {
	data      any
	expiresAt time.Time
	fetchedAt time.Time
}

func NewOnDemandCache(ttl time.Duration) *OnDemandCache {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	return &OnDemandCache{ttl: ttl, m: map[string]cacheItem{}}
}

func (c *OnDemandCache) get(key string) (any, bool, bool) {
	if c == nil {
		return nil, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.m[key]
	if !ok {
		return nil, false, false
	}
	return it.data, time.Now().Before(it.expiresAt), true
}

func (c *OnDemandCache) set(key string, data any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.m[key] = cacheItem{data: data, expiresAt: now.Add(c.ttl), fetchedAt: now}
}

// BalancesDTO is GET /traders/{wallet}/balances (§1.3).
type BalancesDTO struct {
	Perp       *PerpBalancesDTO `json:"perp"`
	Spot       *SpotBalancesDTO `json:"spot"`
	DataStatus DataStatus       `json:"data_status"`
}

type PerpBalancesDTO struct {
	AccountValue         *float64   `json:"account_value"`
	TotalNtlPos          *float64   `json:"total_ntl_pos"`
	TotalMarginUsed      *float64   `json:"total_margin_used"`
	Withdrawable         *float64   `json:"withdrawable"`
	CrossAccountValue    *float64   `json:"cross_account_value"`
	CrossTotalNtlPos     *float64   `json:"cross_total_ntl_pos"`
	CrossTotalMarginUsed *float64   `json:"cross_total_margin_used"`
	AssetPositionsValue  *float64   `json:"asset_positions_value"`
	AsOf                 *time.Time `json:"as_of"`
}

type SpotBalancesDTO struct {
	Balances []SpotBalanceDTO `json:"balances"`
	AsOf     *time.Time       `json:"as_of"`
}

type SpotBalanceDTO struct {
	Coin     string   `json:"coin"`
	Token    string   `json:"token"`
	Total    *float64 `json:"total"`
	Hold     *float64 `json:"hold"`
	EntryNtl *float64 `json:"entry_ntl"`
}

// FillsPage is GET /traders/{wallet}/fills (§1.4). Rows newest-first
// (time DESC, tid DESC); cursor opaque over (time, tid).
type FillsPage struct {
	Rows       []FillDTO `json:"rows"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

type FillDTO struct {
	Coin          string    `json:"coin"`
	Side          string    `json:"side"` // BUY | SELL
	Dir           string    `json:"dir"`
	Size          float64   `json:"size"`
	Price         float64   `json:"price"`
	ClosedPnl     float64   `json:"closed_pnl"`
	Fee           float64   `json:"fee"`
	FeeToken      string    `json:"fee_token"`
	Time          time.Time `json:"time"`
	Tid           int64     `json:"tid"`
	Oid           int64     `json:"oid"`
	Crossed       bool      `json:"crossed"`
	StartPosition string    `json:"start_position"`
}

// OrdersDTO is GET /traders/{wallet}/orders (§1.5).
type OrdersDTO struct {
	Status string     `json:"status"` // open | historical
	Rows   []OrderDTO `json:"rows"`
}

type OrderDTO struct {
	Coin             string     `json:"coin"`
	Side             string     `json:"side"` // BUY | SELL
	LimitPx          float64    `json:"limit_px"`
	Size             float64    `json:"size"`
	OrigSize         float64    `json:"orig_size"`
	Oid              int64      `json:"oid"`
	Timestamp        time.Time  `json:"timestamp"`
	ReduceOnly       bool       `json:"reduce_only"`
	OrderType        string     `json:"order_type"`
	TriggerCondition string     `json:"trigger_condition"`
	TriggerPx        *float64   `json:"trigger_px"`
	IsPositionTpsl   bool       `json:"is_position_tpsl"`
	OrderStatus      *string    `json:"order_status"`
	StatusTimestamp  *time.Time `json:"status_timestamp"`
}

// TransfersPage is GET /traders/{wallet}/transfers (§1.6). Rows newest-first
// (time DESC, hash ASC); cursor opaque over (time, hash).
type TransfersPage struct {
	Rows       []TransferDTO `json:"rows"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

type TransferDTO struct {
	Time           time.Time `json:"time"`
	Hash           string    `json:"hash"`
	Type           string    `json:"type"`
	Usdc           *float64  `json:"usdc"`
	Token          *string   `json:"token"`
	Amount         *float64  `json:"amount"`
	UsdcValue      *float64  `json:"usdc_value"`
	IsDeposit      *bool     `json:"is_deposit"`
	SourceDex      *string   `json:"source_dex"`
	DestinationDex *string   `json:"destination_dex"`
	Counterparty   *string   `json:"counterparty"`
}

// PerformanceDTO is GET /traders/{wallet}/performance (§1.7).
type PerformanceDTO struct {
	Period  string                 `json:"period"`
	Metrics *PerformanceMetricsDTO `json:"metrics"`
	Equity  []EquityPointDTO       `json:"equity"`
}

type PerformanceMetricsDTO struct {
	ROI            *float64   `json:"roi"`
	PnL            *float64   `json:"pnl"`
	WinRate        *float64   `json:"win_rate"`
	Volume         *float64   `json:"volume"`
	TradeCount     *int64     `json:"trade_count"`
	ProfitFactor   *float64   `json:"profit_factor"`
	MaxDrawdownPct *float64   `json:"max_drawdown_pct"`
	LongWins       *int64     `json:"long_wins"`
	LongCount      *int64     `json:"long_count"`
	ShortWins      *int64     `json:"short_wins"`
	ShortCount     *int64     `json:"short_count"`
	DataStatus     DataStatus `json:"data_status"`
	IsPartial      bool       `json:"is_partial"`
	MetricsAsOf    *time.Time `json:"metrics_as_of"`
}

type EquityPointDTO struct {
	Date        string   `json:"date"`
	EndEquity   *float64 `json:"end_equity"`
	DailyReturn *float64 `json:"daily_return"`
}

// fillCursor is the HMAC-sealed keyset for fills (time DESC, tid DESC).
type fillCursor struct {
	FP string `json:"fp"`
	T  string `json:"t"` // time RFC3339Nano
	I  int64  `json:"i"` // tid
}

func fillsFingerprint(venueID uuid.UUID, addr string) string {
	sum := sha256.Sum256([]byte(venueID.String() + "|fills|" + addr))
	return fmt.Sprintf("%x", sum[:])
}

func encodeFillsCursor(secret []byte, fp string, t time.Time, tid int64) (string, error) {
	raw, err := json.Marshal(fillCursor{FP: fp, T: t.UTC().Format(time.RFC3339Nano), I: tid})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeFillsCursor(secret []byte, fp, raw string) (time.Time, int64, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return time.Time{}, 0, invalidFilter("malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, 0, invalidFilter("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, 0, invalidFilter("malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return time.Time{}, 0, invalidFilter("invalid cursor signature")
	}
	var c fillCursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}, 0, invalidFilter("malformed cursor")
	}
	if c.FP != fp {
		return time.Time{}, 0, invalidFilter("cursor does not match this query")
	}
	tt, err := time.Parse(time.RFC3339Nano, c.T)
	if err != nil {
		return time.Time{}, 0, invalidFilter("malformed cursor")
	}
	return tt.UTC(), c.I, nil
}

// transferCursor is the HMAC-sealed keyset for transfers (time DESC, hash ASC).
type transferCursor struct {
	FP string `json:"fp"`
	T  string `json:"t"`
	H  string `json:"h"`
}

func transfersFingerprint(venueID uuid.UUID, addr string, days int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|transfers|%s|%d", venueID.String(), addr, days)))
	return fmt.Sprintf("%x", sum[:])
}

func encodeTransfersCursor(secret []byte, fp string, t time.Time, hash string) (string, error) {
	raw, err := json.Marshal(transferCursor{FP: fp, T: t.UTC().Format(time.RFC3339Nano), H: hash})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeTransfersCursor(secret []byte, fp, raw string) (time.Time, string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return time.Time{}, "", invalidFilter("invalid cursor signature")
	}
	var c transferCursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	if c.FP != fp {
		return time.Time{}, "", invalidFilter("cursor does not match this query")
	}
	if c.H == "" {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	tt, err := time.Parse(time.RFC3339Nano, c.T)
	if err != nil {
		return time.Time{}, "", invalidFilter("malformed cursor")
	}
	return tt.UTC(), c.H, nil
}
