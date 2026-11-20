package trader

import (
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
)

// MarketDetailHeader is the market context block of Market Detail.
type MarketDetailHeader struct {
	Venue         string     `json:"venue"`
	Market        string     `json:"market"`
	MarkPrice     *float64   `json:"mark_price"`
	IndexPrice    *float64   `json:"index_price"`
	FundingRate   *float64   `json:"funding_rate"`
	NextFundingAt *time.Time `json:"next_funding_at"`
	MarketOI      *float64   `json:"market_oi"`
	LogoURL       string     `json:"logo_url"`
	Category      string     `json:"category"`
	Enabled       bool       `json:"enabled"`
	AsOf          string     `json:"as_of"`
}

// MarketSnapshot is the KPI ribbon of Market Detail.
type MarketSnapshot struct {
	OpenNotionalLong   float64  `json:"open_notional_long"`
	OpenNotionalShort  float64  `json:"open_notional_short"`
	LongShortRatio     float64  `json:"long_short_ratio"`
	UnrealizedPnlLong  float64  `json:"unrealized_pnl_long"`
	UnrealizedPnlShort float64  `json:"unrealized_pnl_short"`
	NetUnrealizedPnl   *float64 `json:"net_unrealized_pnl"`
	TradersLong        int      `json:"traders_long"`
	TradersShort       int      `json:"traders_short"`
	TradersTotal       int      `json:"traders_total"`
	MarginUsedLong     float64  `json:"margin_used_long"`
	MarginUsedShort    float64  `json:"margin_used_short"`
	MarginUsedTotal    *float64 `json:"margin_used_total"`
	AvgLeverageLong    *float64 `json:"avg_leverage_long"`
	AvgLeverageShort   *float64 `json:"avg_leverage_short"`
	MarketOI           *float64 `json:"market_oi"`
	SyncedAt           string   `json:"synced_at"`
}

// MarketDetailCoverage reports coverage counts.
type MarketDetailCoverage struct {
	TrackedWalletsWithPositions int `json:"tracked_wallets_with_positions"`
	TotalWalletsInMarket        int `json:"total_wallets_in_market"`
}

// MarketWalletRow is one row of the market wallets table.
type MarketWalletRow struct {
	Address          string     `json:"address"`
	DisplayName      *string    `json:"display_name"`
	Cohort           string     `json:"cohort"`
	Bias             string     `json:"bias"`
	Volume           float64    `json:"volume"`
	Trades           int        `json:"trades"`
	WinRate          *float64   `json:"win_rate"`
	RealizedPnl      float64    `json:"realized_pnl"`
	LongCount        int64      `json:"long_count"`
	ShortCount       int64      `json:"short_count"`
	LastTradeAt      *time.Time `json:"last_trade_at"`
	PositionSide     string     `json:"position_side"`
	PositionNotional *float64   `json:"position_notional"`
	PositionUpnl     *float64   `json:"position_upnl"`
	PositionLeverage *float64   `json:"position_leverage"`
	MarginUsed       *float64   `json:"margin_used"`
}

// MarketTradeRow is one row of the market trades table.
type MarketTradeRow struct {
	ClosedAt      time.Time `json:"closed_at"`
	WalletAddress string    `json:"wallet_address"`
	DisplayName   *string   `json:"display_name"`
	Side          string    `json:"side"`
	Size          float64   `json:"size"`
	EntryPrice    *float64  `json:"entry_price"`
	ExitPrice     *float64  `json:"exit_price"`
	Volume        float64   `json:"volume"`
	Pnl           float64   `json:"pnl"`
	Fees          float64   `json:"fees"`
	Fills         int       `json:"fills"`
}

// MarketDetailResponse is the GET /markets/:market payload.
type MarketDetailResponse struct {
	Header   MarketDetailHeader   `json:"header"`
	Snapshot MarketSnapshot       `json:"snapshot"`
	Coverage MarketDetailCoverage `json:"coverage"`
}

// MarketWalletsResponse is the GET /markets/:market/wallets payload.
type MarketWalletsResponse struct {
	Data []MarketWalletRow `json:"data"`
	Meta *api.Meta         `json:"meta"`
}

// MarketTradesResponse is the GET /markets/:market/trades payload.
type MarketTradesResponse struct {
	Data []MarketTradeRow `json:"data"`
	Meta *api.Meta        `json:"meta"`
}

// WatchedMarket is one row of watched_markets.
type WatchedMarket struct {
	VenueID   uuid.UUID `json:"-"`
	Venue     string    `json:"venue"`
	Market    string    `json:"market"`
	Enabled   bool      `json:"enabled"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
