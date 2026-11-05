package appsettings

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Setting is one row of app_settings (generic key-value).
type Setting struct {
	Key       string     `json:"key" db:"key"`
	Value     string     `json:"value" db:"value"`
	UpdatedBy *uuid.UUID `json:"updated_by" db:"updated_by"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// TelegramSettings is the typed view of Telegram-related settings.
type TelegramSettings struct {
	MaxWalletsPerUser int `json:"max_wallets_per_user"`
}

// PerpDexSettings is the typed view of the admin-configurable perp dex list.
type PerpDexSettings struct {
	Dexes []string `json:"dexes"`
}

const (
	MinMaxWallets                = 1
	MaxMaxWallets                = 1000
	DefaultMaxWallets            = 50
	SettingKeyTelegramMaxWallets = "telegram_max_wallets_per_user"

	SettingKeyPerpDexes = "perp_dexes"
	MaxPerpDexes        = 1000
)

// Validate checks range 1..1000.
func (t TelegramSettings) Validate() error {
	if t.MaxWalletsPerUser < MinMaxWallets || t.MaxWalletsPerUser > MaxMaxWallets {
		return domain.NewError(domain.ErrCodeValidation, "max_wallets_per_user must be between 1 and 1000")
	}
	return nil
}

// Validate checks the perp dex list: no duplicates, within cap.
// An empty list is valid and means "default dex only".
func (p PerpDexSettings) Validate() error {
	if len(p.Dexes) > MaxPerpDexes {
		return domain.NewError(domain.ErrCodeValidation, fmt.Sprintf("perp_dexes must contain at most %d entries", MaxPerpDexes))
	}
	seen := make(map[string]struct{}, len(p.Dexes))
	for _, d := range p.Dexes {
		d = strings.TrimSpace(d)
		if d == "" {
			return domain.NewError(domain.ErrCodeValidation, "perp dex name must not be empty")
		}
		if _, ok := seen[d]; ok {
			return domain.NewError(domain.ErrCodeValidation, "duplicate perp dex: "+d)
		}
		seen[d] = struct{}{}
	}
	return nil
}
