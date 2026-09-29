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
// ComputedAt is when the backing snapshot was computed (freshness;
// nil = no snapshot).
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
	ComputedAt   *time.Time `json:"computed_at"`
}

// Wallet is the scanner row: identity plus timeframe-scoped metrics
// (Metrics is nil when the wallet has no snapshot for the timeframe), the
// caller's own private tag (Tag is nil when unset) and whether the caller
// starred the wallet (Watchlisted — per-user, WL-*).
type Wallet struct {
	ID          uuid.UUID `json:"id"`
	Chain       string    `json:"chain"`
	Address     string    `json:"address"`
	Dex         string    `json:"dex,omitempty"`
	Tag         *string   `json:"tag"`
	Watchlisted bool      `json:"watchlisted"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Metrics     *Metrics  `json:"metrics"`
}

type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// GroupWallet is the unified group-wallet row: every Wallet field plus the
// membership timestamp. GET /groups/:id/wallets always returns this single
// shape (metrics/tag may be null); JSON readers see a flat object.
type GroupWallet struct {
	Wallet
	AddedAt time.Time `json:"added_at"`
}

// Position is a per-market metric row (POS-*): the detail drawer's
// positions breakdown. Metrics is embedded so its fields serialize flat.
type Position struct {
	Market string `json:"market"`
	Metrics
}

// WalletDetail adds the caller's group memberships (BE-06: never other
// users' memberships) and the per-market positions breakdown (POS-*;
// empty slice when the wallet has no per-market snapshots).
type WalletDetail struct {
	Wallet
	Memberships []GroupRef `json:"memberships"`
	Positions   []Position `json:"positions"`
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
