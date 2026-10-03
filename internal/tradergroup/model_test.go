package tradergroup

import (
	"errors"
	"strings"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func TestCreateGroupRequest_Validate(t *testing.T) {
	if err := (&CreateGroupRequest{Name: "  Alphas  "}).Validate(); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	for name, want := range map[string]domain.ErrorCode{
		"":                       domain.ErrCodeValidation,
		"   ":                    domain.ErrCodeValidation,
		strings.Repeat("x", 101): domain.ErrCodeValidation,
	} {
		var appErr *domain.AppError
		if err := (&CreateGroupRequest{Name: name}).Validate(); !errors.As(err, &appErr) || appErr.Code != want {
			t.Errorf("%q: want %s, got %v", name, want, err)
		}
	}
	if err := (&CreateGroupRequest{Name: strings.Repeat("y", 100)}).Validate(); err != nil {
		t.Errorf("100-rune name must pass: %v", err)
	}
}

func TestUpdateGroupRequest_Validate(t *testing.T) {
	if _, _, err := (&UpdateGroupRequest{}).Validate(); err == nil {
		t.Error("empty update must fail")
	}
	name := "  New  "
	rename, desc, err := (&UpdateGroupRequest{Name: &name}).Validate()
	if err != nil || *rename != "New" || desc != nil {
		t.Errorf("rename: %v %v %v", rename, desc, err)
	}
	empty := "  "
	if _, _, err := (&UpdateGroupRequest{Name: &empty}).Validate(); err == nil {
		t.Error("blank name must fail")
	}
}

func strPtr(s string) *string { return &s }

// GRP-M-01: absent keeps, present sets, present-empty clears; caps enforced.
func TestUpdateMemberInput_Validate(t *testing.T) {
	v, err := UpdateMemberInput{
		Venue: "Hyperliquid", WalletAddress: "0xABCDEF0123456789abcdef0123456789ABCDEF01",
	}.Validate()
	if err != nil {
		t.Fatalf("minimal: %v", err)
	}
	if v.Venue != "hyperliquid" || v.WalletAddress != "0xabcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("normalize: %+v", v)
	}
	if v.SetAlias || v.SetNote {
		t.Errorf("absent must not set: %+v", v)
	}

	v, err = UpdateMemberInput{
		Venue: "hyperliquid", WalletAddress: "0xabcdef0123456789abcdef0123456789abcdef01",
		Alias: strPtr("  whale  "), Note: strPtr(""),
	}.Validate()
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	if !v.SetAlias || v.Alias != "whale" || !v.SetNote || v.Note != "" {
		t.Errorf("tri-state: %+v", v)
	}

	badVenue := UpdateMemberInput{WalletAddress: "0xabcdef0123456789abcdef0123456789abcdef01"}
	if _, err := badVenue.Validate(); err == nil {
		t.Error("missing venue must fail")
	}
	badAddr := UpdateMemberInput{Venue: "hyperliquid", WalletAddress: "zzz"}
	if _, err := badAddr.Validate(); err == nil {
		t.Error("bad address must fail")
	}
	long := strings.Repeat("x", 101)
	if _, err := (UpdateMemberInput{Venue: "hyperliquid",
		WalletAddress: "0xabcdef0123456789abcdef0123456789abcdef01",
		Alias:         &long}).Validate(); err == nil {
		t.Error("alias over cap must fail")
	}
	longNote := strings.Repeat("y", 501)
	if _, err := (UpdateMemberInput{Venue: "hyperliquid",
		WalletAddress: "0xabcdef0123456789abcdef0123456789abcdef01",
		Note:          &longNote}).Validate(); err == nil {
		t.Error("note over cap must fail")
	}
}
