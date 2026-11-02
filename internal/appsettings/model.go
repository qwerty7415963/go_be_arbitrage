package appsettings

import (
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

const (
	MinMaxWallets                = 1
	MaxMaxWallets                = 1000
	DefaultMaxWallets            = 50
	SettingKeyTelegramMaxWallets = "telegram_max_wallets_per_user"
)

// Validate checks range 1..1000.
func (t TelegramSettings) Validate() error {
	if t.MaxWalletsPerUser < MinMaxWallets || t.MaxWalletsPerUser > MaxMaxWallets {
		return domain.NewError(domain.ErrCodeValidation, "max_wallets_per_user must be between 1 and 1000")
	}
	return nil
}
