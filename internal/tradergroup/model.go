package tradergroup

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

var addrRe = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// MaxNameRunes caps group names (parity with the legacy groups API).
const MaxNameRunes = 100

// MaxAliasRunes caps member aliases; MaxNoteRunes caps member notes (BE-2).
const (
	MaxAliasRunes = 100
	MaxNoteRunes  = 500
)

// Group is one row of trader_groups: user-owned, name unique per user.
type Group struct {
	ID            uuid.UUID `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	IsFavorite    bool      `json:"is_favorite"`
	NotifyEnabled bool      `json:"notify_enabled"`
	MemberCount   int       `json:"member_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GroupRef is a minimal group reference used in in_groups.
type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// GroupListItem is one row of the groups list (contract §6.1).
type GroupListItem struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	IsFavorite     bool      `json:"is_favorite"`
	NotifyEnabled  bool      `json:"notify_enabled"`
	MemberCount    int       `json:"member_count"`
	AccountValue   *float64  `json:"account_value"`
	UnrealizedPnL  *float64  `json:"unrealized_pnl"`
	ROI            *float64  `json:"roi"`
	Volume         *float64  `json:"volume"`
	OpenPositions  int       `json:"open_positions"`
	WinRate        *float64  `json:"win_rate"`
	UpdatedAt      time.Time `json:"updated_at"`
	DataStatus     string    `json:"data_status"`
	Partial        bool      `json:"partial"`
	MissingWallets int       `json:"missing_wallets"`
}

// Member is one row of trader_group_members: (group, venue, address) with an
// optional alias/note. Metrics are never copied here (spec A8); the Metrics
// field is populated per-request from the period cache (null when the wallet
// has no metrics for the period �?" same semantics as trader detail).
type Member struct {
	GroupID       uuid.UUID             `json:"group_id"`
	VenueID       uuid.UUID             `json:"venue_id"`
	Venue         string                `json:"venue"`
	WalletAddress string                `json:"wallet_address"`
	DisplayName   *string               `json:"display_name"`
	Alias         *string               `json:"alias"`
	Note          *string               `json:"note"`
	Metrics       *trader.PeriodMetrics `json:"metrics"`
}

// MemberRow is one row of the members list (contract §6.3).
type MemberRow struct {
	VenueID       uuid.UUID  `json:"venue_id"`
	Venue         string     `json:"venue"`
	WalletAddress string     `json:"wallet_address"`
	DisplayName   *string    `json:"display_name"`
	Alias         *string    `json:"alias"`
	Note          *string    `json:"note"`
	AccountValue  *float64   `json:"account_value"`
	UnrealizedPnL *float64   `json:"unrealized_pnl"`
	ROI           *float64   `json:"roi"`
	Volume        *float64   `json:"volume"`
	OpenPositions int        `json:"open_positions"`
	LastTradeAt   *time.Time `json:"last_trade_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DataStatus    string     `json:"data_status"`
	InGroups      []GroupRef `json:"in_groups"`
}

// OverviewStats is the groups overview aggregate (contract §6.1).
type OverviewStats struct {
	TotalGroups    int       `json:"total_groups"`
	UniqueWallets  int       `json:"unique_wallets"`
	AccountValue   *float64  `json:"account_value"`
	UnrealizedPnL  *float64  `json:"unrealized_pnl"`
	Partial        bool      `json:"partial"`
	MissingWallets int       `json:"missing_wallets"`
	StaleGroups    int       `json:"stale_groups"`
	EmptyGroups    int       `json:"empty_groups"`
	AsOf           time.Time `json:"as_of"`
}

// GroupStats is the group stats response (contract A6.4).
type GroupStatWallet struct {
	WalletAddress string     `json:"wallet_address"`
	Venue         string     `json:"venue"`
	DataStatus    string     `json:"data_status"`
	AsOf          *time.Time `json:"as_of"`
	HasPositions  bool       `json:"has_positions"`
	HasEquity     bool       `json:"has_equity"`
	MissingReason *string    `json:"missing_reason"`
}

type GroupStats struct {
	Period             string            `json:"period"`
	From               time.Time         `json:"from"`
	To                 time.Time         `json:"to"`
	AsOf               *time.Time        `json:"as_of"`
	EvaluatedAt        time.Time         `json:"evaluated_at"`
	DataStatus         string            `json:"data_status"`
	Partial            bool              `json:"partial"`
	MembersTotal       int               `json:"members_total"`
	MembersWithData    int               `json:"members_with_data"`
	MissingWallets     int               `json:"missing_wallets"`
	AccountValue       *float64          `json:"account_value"`
	TotalNotional      *float64          `json:"total_notional"`
	UnrealizedPnL      *float64          `json:"unrealized_pnl"`
	RealizedPnL        *float64          `json:"realized_pnl"`
	ROI                *float64          `json:"roi"`
	WinCount           *int64            `json:"win_count"`
	LossCount          *int64            `json:"loss_count"`
	BreakevenCount     *int64            `json:"breakeven_count"`
	TradeCount         *int64            `json:"trade_count"`
	WinRate            *float64          `json:"win_rate"`
	Volume             *float64          `json:"volume"`
	ProfitFactor       *float64          `json:"profit_factor"`
	MaxDrawdownPct     *float64          `json:"max_drawdown_pct"`
	OpenPositions      int               `json:"open_positions"`
	DefinitionsVersion int               `json:"definitions_version"`
	Wallets            []GroupStatWallet `json:"wallets"`
}

// PositionTotals aggregates position notional across a group.
type PositionTotals struct {
	LongNtl       float64 `json:"long_ntl"`
	ShortNtl      float64 `json:"short_ntl"`
	NetNtl        float64 `json:"net_ntl"`
	GrossNtl      float64 `json:"gross_ntl"`
	OpenPositions int     `json:"open_positions"`
}

// PositionWallet is one wallet's contribution to a coin position.
type PositionWallet struct {
	WalletAddress            string    `json:"wallet_address"`
	DisplayName              *string   `json:"display_name"`
	Alias                    *string   `json:"alias"`
	Side                     string    `json:"side"`
	Size                     float64   `json:"size"`
	EntryPrice               *float64  `json:"entry_price"`
	MarkPrice                *float64  `json:"mark_price"`
	Leverage                 *float64  `json:"leverage"`
	LiquidationPrice         *float64  `json:"liquidation_price"`
	DistanceToLiquidationPct *float64  `json:"distance_to_liquidation_pct"`
	PositionValue            *float64  `json:"position_value"`
	UnrealizedPnL            *float64  `json:"unrealized_pnl"`
	AsOf                     time.Time `json:"as_of"`
	Stale                    bool      `json:"stale"`
}

// PositionRow aggregates positions by coin.
type PositionRow struct {
	Coin          string           `json:"coin"`
	LongNtl       float64          `json:"long_ntl"`
	ShortNtl      float64          `json:"short_ntl"`
	NetNtl        float64          `json:"net_ntl"`
	GrossNtl      float64          `json:"gross_ntl"`
	WalletCount   int              `json:"wallet_count"`
	UnrealizedPnL *float64         `json:"unrealized_pnl"`
	Wallets       []PositionWallet `json:"wallets"`
}

// PositionsResponse is GET .../positions.
type PositionsResponse struct {
	AsOf           time.Time      `json:"as_of"`
	DataStatus     string         `json:"data_status"`
	Partial        bool           `json:"partial"`
	MissingWallets int            `json:"missing_wallets"`
	Totals         PositionTotals `json:"totals"`
	Positions      []PositionRow  `json:"positions"`
}

// ExposureRow is one coin's exposure aggregation (contract A6.6.1).
type ExposureRow struct {
	Coin         string   `json:"coin"`
	LongNtl      float64  `json:"long_ntl"`
	ShortNtl     float64  `json:"short_ntl"`
	NetNtl       float64  `json:"net_ntl"`
	GrossNtl     float64  `json:"gross_ntl"`
	LongWallets  int      `json:"long_wallets"`
	ShortWallets int      `json:"short_wallets"`
	AvgLeverage  *float64 `json:"avg_leverage"`
}

// ExposureResponse is GET .../exposure.
type ExposureResponse struct {
	Period         string        `json:"period"`
	AsOf           *time.Time    `json:"as_of"`
	DataStatus     string        `json:"data_status"`
	Partial        bool          `json:"partial"`
	MissingWallets int           `json:"missing_wallets"`
	Rows           []ExposureRow `json:"rows"`
}

// PerformancePoint is one point in a performance series.
type PerformancePoint struct {
	T                     time.Time `json:"t"`
	AccountValue          *float64  `json:"account_value"`
	CumulativeRealizedPnL *float64  `json:"cumulative_realized_pnl"`
	ROIIndex              *float64  `json:"roi_index"`
}

// PerformanceMetricAvailability reports which metrics have data.
type PerformanceMetricAvailability struct {
	AccountValue          bool `json:"account_value"`
	CumulativeRealizedPnL bool `json:"cumulative_realized_pnl"`
	ROI                   bool `json:"roi"`
}

// PerformanceResponse is GET .../performance.
type PerformanceResponse struct {
	Period             string                        `json:"period"`
	Granularity        string                        `json:"granularity"`
	From               time.Time                     `json:"from"`
	To                 time.Time                     `json:"to"`
	Partial            bool                          `json:"partial"`
	MissingWallets     int                           `json:"missing_wallets"`
	InsufficientData   bool                          `json:"insufficient_data"`
	MetricAvailability PerformanceMetricAvailability `json:"metric_availability"`
	Points             []PerformancePoint            `json:"points"`
}

// ComparePoint is one ROI-index point in comparison.
type ComparePoint struct {
	T        time.Time `json:"t"`
	ROIIndex *float64  `json:"roi_index"`
}

// CompareSeries is one group's series in comparison.
type CompareSeries struct {
	GroupID uuid.UUID      `json:"group_id"`
	Points  []ComparePoint `json:"points"`
}

// CompareOverlapPair counts shared wallets between two groups.
type CompareOverlapPair struct {
	A             uuid.UUID `json:"a"`
	B             uuid.UUID `json:"b"`
	SharedWallets int       `json:"shared_wallets"`
}

// CompareOverlap is the overlap block.
type CompareOverlap struct {
	Pairwise []CompareOverlapPair `json:"pairwise"`
}

// CompareGroup is one group in comparison.
type CompareGroup struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	MemberCount int        `json:"member_count"`
	Stats       GroupStats `json:"stats"`
}

// CompareResponse is POST .../compare.
type CompareResponse struct {
	Period   string          `json:"period"`
	Groups   []CompareGroup  `json:"groups"`
	Series   []CompareSeries `json:"series"`
	Overlap  CompareOverlap  `json:"overlap"`
	Warnings []string        `json:"warnings"`
	AsOf     time.Time       `json:"as_of"`
}

// ListGroupsParams filters the groups list.
type ListGroupsParams struct {
	Search   string
	Favorite *bool
	Status   string
	Period   string
	Sort     string
	Dir      string
	Page     int
	PageSize int
}

// ListMembersParams filters the members list.
type ListMembersParams struct {
	Period   string
	Search   string
	Sort     string
	Dir      string
	Page     int
	PageSize int
	Filter   string
}

// CreateGroupRequest is POST /v1/trader-groups.
type CreateGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateGroupRequest is PATCH /v1/trader-groups/{id}.
type UpdateGroupRequest struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	IsFavorite    *bool   `json:"is_favorite"`
	NotifyEnabled *bool   `json:"notify_enabled"`
}

// DuplicateGroupRequest is POST /v1/trader-groups/{id}/duplicate.
type DuplicateGroupRequest struct {
	Name string `json:"name"`
}

// Validate enforces name length 1..100 runes when provided.
func (r *DuplicateGroupRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(r.Name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > MaxNameRunes {
		return domain.NewError(domain.ErrCodeValidation, "name must be 1..100 characters")
	}
	return nil
}

// FavoriteRequest is PATCH /v1/trader-groups/{id}/favorite.
type FavoriteRequest struct {
	IsFavorite bool `json:"is_favorite"`
}

// CompareRequest is POST /v1/trader-groups/compare (contract §6.8).
type CompareRequest struct {
	GroupIDs []string `json:"group_ids"`
	Period   string   `json:"period"`
}

// AddMembersRequest is POST /v1/trader-groups/{id}/members.
type AddMembersRequest struct {
	Members []MemberInput `json:"members"`
}

// MemberInput identifies one wallet: venue + address, optional alias/note.
type MemberInput struct {
	Venue         string `json:"venue"`
	WalletAddress string `json:"wallet_address"`
	Alias         string `json:"alias"`
	Note          string `json:"note"`
}

// UpdateMembersRequest is PATCH /v1/trader-groups/{id}/members.
type UpdateMembersRequest struct {
	Members []UpdateMemberInput `json:"members"`
}

// UpdateMemberInput edits one membership's alias/note. Pointers distinguish
// absent (keep) from present (set; trimmed, empty clears to NULL).
type UpdateMemberInput struct {
	Venue         string  `json:"venue"`
	WalletAddress string  `json:"wallet_address"`
	Alias         *string `json:"alias"`
	Note          *string `json:"note"`
}

// ValidatedMemberUpdate is a normalized PATCH item. Set flags distinguish
// absent (keep the stored value) from present (set it; trimmed, empty clears
// to NULL).
type ValidatedMemberUpdate struct {
	Venue         string
	WalletAddress string
	SetAlias      bool
	Alias         string
	SetNote       bool
	Note          string
}

// Validate trims and checks caps, returning the normalized tri-state update.
func (in UpdateMemberInput) Validate() (ValidatedMemberUpdate, error) {
	var out ValidatedMemberUpdate
	out.Venue = strings.ToLower(strings.TrimSpace(in.Venue))
	if out.Venue == "" {
		return out, domain.NewError(domain.ErrCodeValidation, "member venue is required")
	}
	out.WalletAddress = strings.ToLower(strings.TrimSpace(in.WalletAddress))
	if !addrRe.MatchString(out.WalletAddress) {
		return out, domain.NewError(domain.ErrCodeValidation, "invalid member address")
	}
	if in.Alias != nil {
		trimmed := strings.TrimSpace(*in.Alias)
		if utf8.RuneCountInString(trimmed) > MaxAliasRunes {
			return out, domain.NewError(domain.ErrCodeValidation, "alias exceeds 100 characters")
		}
		out.SetAlias = true
		out.Alias = trimmed
	}
	if in.Note != nil {
		trimmed := strings.TrimSpace(*in.Note)
		if utf8.RuneCountInString(trimmed) > MaxNoteRunes {
			return out, domain.NewError(domain.ErrCodeValidation, "note exceeds 500 characters")
		}
		out.SetNote = true
		out.Note = trimmed
	}
	return out, nil
}

func (r *CreateGroupRequest) Validate() error {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return domain.NewError(domain.ErrCodeValidation, "name is required")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return domain.NewError(domain.ErrCodeValidation, "name exceeds 100 characters")
	}
	return nil
}

// ValidateUpdate requires at least one field; returns the trimmed values.
func (r *UpdateGroupRequest) Validate() (name, description *string, isFavorite, notifyEnabled *bool, err error) {
	if r.Name == nil && r.Description == nil && r.IsFavorite == nil && r.NotifyEnabled == nil {
		return nil, nil, nil, nil, domain.NewError(domain.ErrCodeValidation,
			"at least one of name, description, is_favorite, notify_enabled is required")
	}
	if r.Name != nil {
		trimmed := strings.TrimSpace(*r.Name)
		if trimmed == "" {
			return nil, nil, nil, nil, domain.NewError(domain.ErrCodeValidation, "name must not be empty")
		}
		if utf8.RuneCountInString(trimmed) > MaxNameRunes {
			return nil, nil, nil, nil, domain.NewError(domain.ErrCodeValidation, "name exceeds 100 characters")
		}
		name = &trimmed
	}
	if r.Description != nil {
		trimmed := strings.TrimSpace(*r.Description)
		description = &trimmed
	}
	if r.IsFavorite != nil {
		v := *r.IsFavorite
		isFavorite = &v
	}
	if r.NotifyEnabled != nil {
		v := *r.NotifyEnabled
		notifyEnabled = &v
	}
	return name, description, isFavorite, notifyEnabled, nil
}

// ActivityEvent is one row in the group activity timeline (contract v1.0 §6.7).
type ActivityEvent struct {
	ID            int64          `json:"id"`
	Type          string         `json:"type"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Venue         string         `json:"venue"`
	WalletAddress string         `json:"wallet_address"`
	Coin          string         `json:"coin"`
	SizeDelta     *float64       `json:"size_delta"`
	Price         *float64       `json:"price"`
	RealizedPnl   *float64       `json:"realized_pnl"`
	Meta          map[string]any `json:"meta,omitempty"`
}

// ActivityPage is the response for GET /{id}/activity.
type ActivityPage struct {
	Items      []ActivityEvent `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
	Partial    bool            `json:"partial"`
	Sources    map[string]bool `json:"sources"`
}

// ActivityFilter holds query params for Activity.
type ActivityFilter struct {
	Wallet string
	Asset  string
	Type   string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
}

// GroupAlert is one configured alert for a group.
type GroupAlert struct {
	ID               uuid.UUID      `json:"id"`
	GroupID          uuid.UUID      `json:"group_id"`
	UserID           uuid.UUID      `json:"user_id"`
	Name             string         `json:"name"`
	Type             string         `json:"type"`
	Params           map[string]any `json:"params"`
	Enabled          bool           `json:"enabled"`
	CooldownSeconds  int            `json:"cooldown_seconds"`
	NotifyInApp      bool           `json:"notify_in_app"`
	NotifyTelegram   bool           `json:"notify_telegram"`
	LastConditionMet bool           `json:"last_condition_met"`
	LastValue        *float64       `json:"last_value"`
	LastTriggeredAt  *time.Time     `json:"last_triggered_at"`
	LastEvaluatedAt  *time.Time     `json:"last_evaluated_at"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	UnreadEvents     int            `json:"unread_events"`
}

// AlertEvent is one triggered alert occurrence.
type AlertEvent struct {
	ID          uuid.UUID      `json:"id"`
	AlertID     uuid.UUID      `json:"alert_id"`
	GroupID     uuid.UUID      `json:"group_id"`
	Type        string         `json:"type"`
	Payload     map[string]any `json:"payload"`
	TriggeredAt time.Time      `json:"triggered_at"`
	ReadAt      *time.Time     `json:"read_at"`
}

// CreateAlertRequest is POST /{id}/alerts.
type CreateAlertRequest struct {
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Params          map[string]any `json:"params"`
	Enabled         *bool          `json:"enabled"`
	CooldownSeconds *int           `json:"cooldown_seconds"`
	NotifyInApp     *bool          `json:"notify_in_app"`
	NotifyTelegram  *bool          `json:"notify_telegram"`
}

// UpdateAlertRequest is PATCH /trader-alerts/{id}.
type UpdateAlertRequest struct {
	Name            *string        `json:"name"`
	Params          map[string]any `json:"params"`
	Enabled         *bool          `json:"enabled"`
	CooldownSeconds *int           `json:"cooldown_seconds"`
	NotifyInApp     *bool          `json:"notify_in_app"`
	NotifyTelegram  *bool          `json:"notify_telegram"`
}

type AlertEventPage struct {
	Items       []AlertEvent `json:"items"`
	NextCursor  string       `json:"next_cursor,omitempty"`
	HasMore     bool         `json:"has_more"`
	UnreadCount int          `json:"unread_count"`
}
