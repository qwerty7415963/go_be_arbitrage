package walletgroup

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Group struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	UserID      uuid.UUID  `json:"-" db:"user_id"`
	Name        string     `json:"name" db:"name"`
	Description *string    `json:"description,omitempty" db:"description"`
	Color       *string    `json:"color,omitempty" db:"color"`
	WalletCount int64      `json:"wallet_count" db:"wallet_count"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

type WalletRef struct {
	ID      uuid.UUID `json:"id" db:"id"`
	Chain   string    `json:"chain" db:"chain"`
	Address string    `json:"address" db:"address"`
	AddedAt time.Time `json:"added_at" db:"added_at"`
}

type CreateGroupRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

type UpdateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

// WalletsRequest is the body for POST/DELETE /groups/:id/wallets.
// Entries are either wallet IDs (uuid strings) or raw addresses; raw
// addresses are normalized and resolved to their tracked_wallets identity.
type WalletsRequest struct {
	Wallets []string `json:"wallets" binding:"required,min=1"`
	Chain   string   `json:"chain"`
}

type walletItem struct {
	isID    bool
	id      uuid.UUID
	chain   string
	address string
}

var (
	evmAddressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	chainRe      = regexp.MustCompile(`^[a-z0-9:._-]+$`)
)

// NormalizeAddress returns the canonical wallet identity for an address:
// trimmed, 0x-prefixed, lowercase hex (BR-01). Invalid input yields
// WALLET-002.
func NormalizeAddress(addr string) (string, error) {
	a := strings.ToLower(strings.TrimSpace(addr))
	if !evmAddressRe.MatchString(a) {
		return "", domain.NewError(domain.ErrCodeWalletInvalidAddress, "invalid wallet address format")
	}
	return a, nil
}

// NormalizeChain returns the canonical chain identifier: trimmed, lowercase,
// numeric chains without the 0x prefix ("0x1" == "1"). Invalid input yields
// WALLET-003.
func NormalizeChain(chain string) (string, error) {
	c := strings.ToLower(strings.TrimSpace(chain))
	if c == "" {
		return "evm", nil
	}
	if strings.HasPrefix(c, "0x") {
		rest := c[2:]
		if rest != "" && strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			c = rest
		}
	}
	if !chainRe.MatchString(c) {
		return "", domain.NewError(domain.ErrCodeWalletUnsupportedChain, "unsupported chain identifier")
	}
	return c, nil
}

// ValidateGroupName trims the name and reports whether it is blank
// (COMMON-902 field error on "name").
func ValidateGroupName(name string) (string, *api.FieldError) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", &api.FieldError{
			Field:   "name",
			Code:    string(domain.ErrCodeValidation),
			Message: "name must not be blank",
		}
	}
	return trimmed, nil
}

// parseWalletItems normalizes each entry of a wallets array into either a
// wallet ID lookup or an address identity. Format problems yield WALLET-002.
func parseWalletItems(entries []string, chain string) ([]walletItem, error) {
	normalizedChain, err := NormalizeChain(chain)
	if err != nil {
		return nil, err
	}

	items := make([]walletItem, 0, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, domain.NewError(domain.ErrCodeWalletInvalidAddress, "wallet entry must not be blank")
		}
		if id, err := uuid.Parse(entry); err == nil {
			items = append(items, walletItem{isID: true, id: id})
			continue
		}
		addr, err := NormalizeAddress(entry)
		if err != nil {
			return nil, err
		}
		items = append(items, walletItem{chain: normalizedChain, address: addr})
	}
	return items, nil
}
