package telegram

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

const CodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const CodeLength = 8

// Locale is the message language captured at bind time.
type Locale string

const (
	LocaleVI Locale = "vi"
	LocaleEN Locale = "en"
)

func (l Locale) IsValid() bool {
	return l == LocaleVI || l == LocaleEN
}

func (l Locale) String() string { return string(l) }

// BindingStatus values.
type BindingStatus string

const (
	BindingStatusActive  BindingStatus = "active"
	BindingStatusBlocked BindingStatus = "blocked"
)

func (s BindingStatus) IsValid() bool {
	return s == BindingStatusActive || s == BindingStatusBlocked
}

// Binding is one Telegram chat bound to a user account.
type Binding struct {
	UserID      uuid.UUID     `json:"user_id" db:"user_id"`
	ChatID      int64         `json:"chat_id" db:"chat_id"`
	TGUsername  *string       `json:"tg_username" db:"tg_username"`
	TGFirstName *string       `json:"tg_first_name" db:"tg_first_name"`
	Locale      Locale        `json:"locale" db:"locale"`
	Status      BindingStatus `json:"status" db:"status"`
	CreatedAt   time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at" db:"updated_at"`
}

func (b *Binding) Validate() error {
	if b == nil {
		return errors.New("binding is nil")
	}
	if b.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if b.ChatID == 0 {
		return errors.New("chat_id is required")
	}
	if !b.Locale.IsValid() {
		return errors.New("invalid locale")
	}
	if !b.Status.IsValid() {
		return errors.New("invalid status")
	}
	return nil
}

// LinkCode is a one-time code used to bind a Telegram chat.
type LinkCode struct {
	Code      string     `json:"code" db:"code"`
	UserID    uuid.UUID  `json:"user_id" db:"user_id"`
	Locale    Locale     `json:"locale" db:"locale"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	UsedAt    *time.Time `json:"used_at" db:"used_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

func (c *LinkCode) Validate() error {
	if c == nil {
		return errors.New("link code is nil")
	}
	if err := ValidateCode(c.Code); err != nil {
		return err
	}
	if c.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if !c.Locale.IsValid() {
		return errors.New("invalid locale")
	}
	if c.ExpiresAt.IsZero() {
		return errors.New("expires_at is required")
	}
	return nil
}

// ValidateCode checks length and alphabet.
func ValidateCode(code string) error {
	if len(code) != CodeLength {
		return errors.New("code must be 8 characters")
	}
	for _, r := range code {
		if !strings.ContainsRune(CodeAlphabet, r) {
			return errors.New("code contains invalid character")
		}
	}
	return nil
}

func IsValidCode(code string) bool { return ValidateCode(code) == nil }

// GenerateCode returns a random 8-char code from CodeAlphabet.
func GenerateCode() (string, error) {
	out := make([]byte, CodeLength)
	alphabetLen := big.NewInt(int64(len(CodeAlphabet)))
	for i := 0; i < CodeLength; i++ {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", err
		}
		out[i] = CodeAlphabet[n.Int64()]
	}
	return string(out), nil
}

// GenerateLinkCode is an alias for GenerateCode.
func GenerateLinkCode() (string, error) { return GenerateCode() }
