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
