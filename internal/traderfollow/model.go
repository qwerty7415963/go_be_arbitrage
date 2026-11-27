package traderfollow

import (
	"time"

	"github.com/google/uuid"
)

type FollowKey struct {
	Venue         string  `json:"venue"`
	WalletAddress string  `json:"wallet_address"`
	Alias         *string `json:"alias,omitempty"`
}

type FollowListItem struct {
	VenueID       uuid.UUID `json:"venue_id"`
	Venue         string    `json:"venue"`
	WalletAddress string    `json:"wallet_address"`
	DisplayName   *string   `json:"display_name"`
	Alias         *string   `json:"alias"`
	NotifyEnabled bool      `json:"notify_enabled"`
	FollowedAt    time.Time `json:"followed_at"`
	InGroups      []string  `json:"in_groups"`

	AccountValue  *float64   `json:"account_value"`
	UnrealizedPnL *float64   `json:"unrealized_pnl"`
	ROI           *float64   `json:"roi"`
	Volume        *float64   `json:"volume"`
	OpenPositions int        `json:"open_positions"`
	LastTradeAt   *time.Time `json:"last_trade_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DataStatus    string     `json:"data_status"`

	PnL          *float64 `json:"pnl"`
	RealizedPnL  *float64 `json:"realized_pnl"`
	WinRate      *float64 `json:"win_rate"`
	TradeCount   *int64   `json:"trade_count"`
	ProfitFactor *float64 `json:"profit_factor"`
	MaxDrawdown  *float64 `json:"max_drawdown_pct"`
}

type FollowOverview struct {
	FollowingCount   int       `json:"following_count"`
	GroupsCount      int       `json:"groups_count"`
	UniqueWallets    int       `json:"unique_wallets"`
	OverlapWallets   int       `json:"overlap_wallets"`
	GroupOnlyWallets int       `json:"group_only_wallets"`
	AccountValue     *float64  `json:"account_value"`
	UnrealizedPnL    *float64  `json:"unrealized_pnl"`
	Partial          bool      `json:"partial"`
	AsOf             time.Time `json:"as_of"`
}

type ListFollowsParams struct {
	Period   string
	Search   string
	Sort     string
	Dir      string
	Page     int
	PageSize int
	Filter   string
}

type MemberInput struct {
	Venue         string  `json:"venue"`
	WalletAddress string  `json:"wallet_address"`
	NotifyEnabled *bool   `json:"notify_enabled"`
	Alias         *string `json:"alias"`
}

type UpdateFollowRequest struct {
	NotifyEnabled *bool   `json:"notify_enabled"`
	Alias         *string `json:"alias"`
}

type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type FollowStatus struct {
	IsFollowed        bool       `json:"is_followed"`
	NotifyEnabled     bool       `json:"notify_enabled"`
	Alias             *string    `json:"alias"`
	DisplayName       *string    `json:"display_name"`
	InGroups          []GroupRef `json:"in_groups"`
	FollowedAt        *time.Time `json:"followed_at"`
	TelegramConnected bool       `json:"telegram_connected"`
}
