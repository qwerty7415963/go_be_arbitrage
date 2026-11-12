package trader

// TraderOverview is the trader overview aggregate.
type TraderOverview struct {
	AsOf           string                  `json:"as_of"`
	Venue          string                  `json:"venue"`
	Period         string                  `json:"period"`
	PnlMetric      string                  `json:"pnl_metric"`
	UniverseSize   int                     `json:"universe_size"`
	PnlBuckets     []OverviewBucket        `json:"pnl_buckets"`
	AccountBuckets []OverviewAccountBucket `json:"account_buckets"`
	Coverage       OverviewCoverage        `json:"coverage"`
}

// OverviewBucket is one PnL bucket.
type OverviewBucket struct {
	ID            string          `json:"id"`
	MinPnL        *float64        `json:"min_pnl"`
	MaxPnL        *float64        `json:"max_pnl"`
	WalletCount   int             `json:"wallet_count"`
	PnlTotal      float64         `json:"pnl_total"`
	VolumeTotal   float64         `json:"volume_total"`
	LongCount     int64           `json:"long_count"`
	ShortCount    int64           `json:"short_count"`
	LongRatio     float64         `json:"long_ratio"`
	LongNotional  float64         `json:"long_notional"`
	ShortNotional float64         `json:"short_notional"`
	Sentiment     string          `json:"sentiment"`
	Series        []OverviewPoint `json:"series"`
}

// OverviewAccountBucket is one account bucket.
type OverviewAccountBucket struct {
	ID            string          `json:"id"`
	MinEquity     *float64        `json:"min_equity"`
	MaxEquity     *float64        `json:"max_equity"`
	WalletCount   int             `json:"wallet_count"`
	EquityTotal   float64         `json:"equity_total"`
	VolumeTotal   float64         `json:"volume_total"`
	LongCount     int64           `json:"long_count"`
	ShortCount    int64           `json:"short_count"`
	LongRatio     float64         `json:"long_ratio"`
	LongNotional  float64         `json:"long_notional"`
	ShortNotional float64         `json:"short_notional"`
	Sentiment     string          `json:"sentiment"`
	Series        []OverviewPoint `json:"series"`
}

// OverviewPoint is one series point.
type OverviewPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// OverviewCoverage is coverage counts.
type OverviewCoverage struct {
	PnlWallets          int            `json:"pnl_wallets"`
	AccountValueWallets int            `json:"account_value_wallets"`
	SeriesWallets       int            `json:"series_wallets"`
	DataStatus          map[string]int `json:"data_status"`
}

// BucketDetail is the bucket detail aggregate.
type BucketDetail struct {
	AsOf          string                   `json:"as_of"`
	Kind          string                   `json:"kind"`
	BucketID      string                   `json:"bucket_id"`
	WalletCount   int                      `json:"wallet_count"`
	Sentiment     string                   `json:"sentiment"`
	LongRatio     float64                  `json:"long_ratio"`
	PnlSplit      BucketPnlSplit           `json:"pnl_split"`
	NotionalSplit BucketNotionalSplit      `json:"notional_split"`
	Positioning   []BucketPositioningPoint `json:"positioning"`
	MarketsTop    []BucketMarketTop        `json:"markets_top"`
	Coverage      BucketDetailCoverage     `json:"coverage"`
}

type BucketPnlSplit struct {
	PctProfit float64 `json:"pct_profit"`
	PctLoss   float64 `json:"pct_loss"`
	UsdProfit float64 `json:"usd_profit"`
	UsdLoss   float64 `json:"usd_loss"`
}

type BucketNotionalSplit struct {
	LongUsd  float64 `json:"long_usd"`
	ShortUsd float64 `json:"short_usd"`
}

type BucketPositioningPoint struct {
	T       string  `json:"t"`
	LongPct float64 `json:"long_pct"`
}

type BucketMarketTop struct {
	Market    string  `json:"market"`
	Notional  float64 `json:"notional"`
	Upnl      float64 `json:"upnl"`
	Sentiment string  `json:"sentiment"`
	Traders   int     `json:"traders"`
}

type BucketDetailCoverage struct {
	WalletsWithPositions int `json:"wallets_with_positions"`
	WalletsWithTrades    int `json:"wallets_with_trades"`
}

type BucketWalletRow struct {
	Address       string  `json:"address"`
	DisplayName   *string `json:"display_name"`
	PerpEquity    float64 `json:"perp_equity"`
	PositionValue float64 `json:"position_value"`
	Leverage      float64 `json:"leverage"`
	WinRate       float64 `json:"win_rate"`
	VolumeRange   float64 `json:"volume_range"`
	RealizedPnl   float64 `json:"realized_pnl"`
	Bias          string  `json:"bias"`
	Cohort        string  `json:"cohort"`

	LongCount  int64 `json:"-"`
	ShortCount int64 `json:"-"`
}

type BucketMarketRow struct {
	Market        string  `json:"market"`
	Sentiment     string  `json:"sentiment"`
	NotionalLong  float64 `json:"notional_long"`
	NotionalShort float64 `json:"notional_short"`
	TradersLong   int     `json:"traders_long"`
	TradersShort  int     `json:"traders_short"`
	PnlProfit     float64 `json:"pnl_profit"`
	PnlLoss       float64 `json:"pnl_loss"`
}
