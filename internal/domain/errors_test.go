package domain

import (
	"fmt"
	"testing"
)

func TestServiceFromCode(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected string
	}{
		{ErrCodeAuthInvalidCredentials, "AUTH"},
		{ErrCodeExchangeConnectionFailed, "EXCHANGE"},
		{ErrCodeOrderNotFound, "ORDER"},
		{ErrCodeRiskKillSwitch, "RISK"},
		{ErrCodeStrategyNotFound, "STRATEGY"},
		{ErrCodeMarketBookStale, "MARKET"},
		{ErrCodeConfigInvalid, "CONFIG"},
		{ErrCodeSyncFailed, "SYNC"},
		{ErrCodeInternal, "COMMON"},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			result := ServiceFromCode(tt.code)
			if result != tt.expected {
				t.Errorf("ServiceFromCode(%s) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestAppError(t *testing.T) {
	err := NewError(ErrCodeAuthInvalidCredentials, "invalid credentials")

	if err.Code != ErrCodeAuthInvalidCredentials {
		t.Errorf("expected code %s, got %s", ErrCodeAuthInvalidCredentials, err.Code)
	}
	if err.Message != "invalid credentials" {
		t.Errorf("expected message 'invalid credentials', got '%s'", err.Message)
	}
	if err.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", err.StatusCode)
	}
}

func TestAppErrorWithDetails(t *testing.T) {
	err := NewError(ErrCodeValidation, "validation failed").
		WithDetails([]string{"field required"})

	if err.Details == nil {
		t.Error("expected details to be set")
	}
}

func TestAppErrorWithErr(t *testing.T) {
	original := fmt.Errorf("original error")
	err := NewError(ErrCodeInternal, "internal error").WithErr(original)

	if err.Err != original {
		t.Error("expected original error to be wrapped")
	}
	if err.Error() != "COMMON-901: internal error: original error" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestIsAppError(t *testing.T) {
	appErr := NewError(ErrCodeNotFound, "not found")
	genericErr := fmt.Errorf("generic error")

	if !IsAppError(appErr) {
		t.Error("expected IsAppError to return true for AppError")
	}
	if IsAppError(genericErr) {
		t.Error("expected IsAppError to return false for generic error")
	}
}

func TestGetAppError(t *testing.T) {
	appErr := NewError(ErrCodeNotFound, "not found")
	genericErr := fmt.Errorf("generic error")

	result := GetAppError(appErr)
	if result.Code != ErrCodeNotFound {
		t.Errorf("expected code %s, got %s", ErrCodeNotFound, result.Code)
	}

	result = GetAppError(genericErr)
	if result.Code != ErrCodeInternal {
		t.Errorf("expected code %s, got %s", ErrCodeInternal, result.Code)
	}
}
