package tradergroup

import (
	"regexp"
	"strings"
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
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MemberCount int       `json:"member_count"`
}

// Member is one row of trader_group_members: (group, venue, address) with an
// optional alias/note. Metrics are never copied here (spec §8); the Metrics
// field is populated per-request from the period cache (null when the wallet
// has no metrics for the period — same semantics as trader detail).
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

// CreateGroupRequest is POST /api/v1/trader-groups.
type CreateGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateGroupRequest is PATCH /api/v1/trader-groups/{id}: name and/or
// description; owner is immutable.
type UpdateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// AddMembersRequest is POST /api/v1/trader-groups/{id}/members.
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

// UpdateMembersRequest is PATCH /api/v1/trader-groups/{id}/members.
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
func (r *UpdateGroupRequest) Validate() (name, description *string, err error) {
	if r.Name == nil && r.Description == nil {
		return nil, nil, domain.NewError(domain.ErrCodeValidation,
			"at least one of name, description is required")
	}
	if r.Name != nil {
		trimmed := strings.TrimSpace(*r.Name)
		if trimmed == "" {
			return nil, nil, domain.NewError(domain.ErrCodeValidation, "name must not be empty")
		}
		if utf8.RuneCountInString(trimmed) > MaxNameRunes {
			return nil, nil, domain.NewError(domain.ErrCodeValidation, "name exceeds 100 characters")
		}
		name = &trimmed
	}
	if r.Description != nil {
		trimmed := strings.TrimSpace(*r.Description)
		description = &trimmed
	}
	return name, description, nil
}
