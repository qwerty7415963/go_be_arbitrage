package wallet

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
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
// (Metrics is nil when the wallet has no snapshot for the timeframe) and
// the caller's own private tag (Tag is nil when unset).
type Wallet struct {
	ID          uuid.UUID `json:"id"`
	Chain       string    `json:"chain"`
	Address     string    `json:"address"`
	Dex         string    `json:"dex,omitempty"`
	Tag         *string   `json:"tag"`
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

// MaxTagRunes bounds a wallet tag (TAG-U-01).
const MaxTagRunes = 100

// NormalizeTag trims a tag update: empty-after-trim means clear (delete the
// row). Over-long tags are COMMON-902.
func NormalizeTag(raw string) (tag string, clear bool, ferr *api.FieldError) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", true, nil
	}
	if utf8.RuneCountInString(trimmed) > MaxTagRunes {
		return "", false, &api.FieldError{
			Field:   "tag",
			Code:    string(domain.ErrCodeValidation),
			Message: "tag must be at most 100 characters",
		}
	}
	return trimmed, false, nil
}
