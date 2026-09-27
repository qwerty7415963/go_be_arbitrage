package wallet

import (
	"time"

	"github.com/google/uuid"
)

// Timeframe keys for metric snapshots (default 30D).
const (
	Timeframe24H = "24H"
	Timeframe7D  = "7D"
	Timeframe30D = "30D"
	Timeframe90D = "90D"
	TimeframeALL = "ALL"
	// TimeframeCustom prefixes custom start/end range snapshot keys.
	TimeframeCustom = "CUSTOM"
)

var validTimeframes = map[string]bool{
	Timeframe24H: true,
	Timeframe7D:  true,
	Timeframe30D: true,
	Timeframe90D: true,
	TimeframeALL: true,
}

// Metrics are nullable: nil means unavailable, never 0 (BR-07).
type Metrics struct {
	RealizedPnl  *float64   `json:"realized_pnl"`
	Roi          *float64   `json:"roi"`
	WinRate      *float64   `json:"win_rate"`
	Volume       *float64   `json:"volume"`
	TradeCount   *int64     `json:"trade_count"`
	AvgPosition  *float64   `json:"avg_position"`
	AvgLeverage  *float64   `json:"avg_leverage"`
	LongCount    *int64     `json:"long_count"`
	ShortCount   *int64     `json:"short_count"`
	LastActiveAt *time.Time `json:"last_active_at"`
}

// Wallet is the scanner row: identity plus timeframe-scoped metrics
// (Metrics is nil when the wallet has no snapshot for the timeframe).
type Wallet struct {
	ID          uuid.UUID `json:"id"`
	Chain       string    `json:"chain"`
	Address     string    `json:"address"`
	Dex         string    `json:"dex,omitempty"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Metrics     *Metrics  `json:"metrics"`
}

type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// WalletDetail adds the caller's group memberships (BE-06: never other
// users' memberships).
type WalletDetail struct {
	Wallet
	Memberships []GroupRef `json:"memberships"`
}

// FilterConfig holds the valid enum values for filters; loaded from the
// database so validation stays data-driven.
type FilterConfig struct {
	Dexes   []string
	Chains  []string
	Markets []string
}

func ValidTimeframe(tf string) bool {
	return validTimeframes[tf]
}
