package exchangeconfig

import (
	"testing"

	"github.com/google/uuid"
)

func TestExchangeConfigModel(t *testing.T) {
	config := &ExchangeConfig{
		ID:            uuid.New(),
		VenueID:       uuid.New(),
		ExchangeName:  "Binance",
		RestBaseURL:   "https://api.binance.com",
		WsURL:         "wss://stream.binance.com:9443",
		Status:        ExchangeConfigStatusActive,
		RateLimitRPM:  1200,
		TimeoutMs:     5000,
	}

	if config.ExchangeName != "Binance" {
		t.Errorf("expected Binance, got %s", config.ExchangeName)
	}
	if config.RestBaseURL != "https://api.binance.com" {
		t.Errorf("expected https://api.binance.com, got %s", config.RestBaseURL)
	}
	if config.Status != ExchangeConfigStatusActive {
		t.Errorf("expected ACTIVE, got %s", config.Status)
	}
	if config.RateLimitRPM != 1200 {
		t.Errorf("expected 1200, got %d", config.RateLimitRPM)
	}
	if config.TimeoutMs != 5000 {
		t.Errorf("expected 5000, got %d", config.TimeoutMs)
	}
}

func TestExchangeConfigStatus(t *testing.T) {
	tests := []struct {
		status   ExchangeConfigStatus
		expected string
	}{
		{ExchangeConfigStatusActive, "ACTIVE"},
		{ExchangeConfigStatusDisabled, "DISABLED"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, string(tt.status))
		}
	}
}

func TestCreateExchangeConfigRequest(t *testing.T) {
	req := CreateExchangeConfigRequest{
		VenueID:      uuid.New(),
		ExchangeName: "Bybit",
		RestBaseURL:  "https://api.bybit.com",
		WsURL:        "wss://stream.bybit.com/v5/public/linear",
		RateLimitRPM: 600,
		TimeoutMs:    3000,
	}

	if req.ExchangeName != "Bybit" {
		t.Errorf("expected Bybit, got %s", req.ExchangeName)
	}
	if req.RateLimitRPM != 600 {
		t.Errorf("expected 600, got %d", req.RateLimitRPM)
	}
}

func TestUpdateExchangeConfigRequest(t *testing.T) {
	newName := "Binance Pro"
	newStatus := ExchangeConfigStatusDisabled

	req := UpdateExchangeConfigRequest{
		ExchangeName: &newName,
		Status:       &newStatus,
	}

	if req.ExchangeName == nil || *req.ExchangeName != "Binance Pro" {
		t.Errorf("expected Binance Pro, got %v", req.ExchangeName)
	}
	if req.Status == nil || *req.Status != ExchangeConfigStatusDisabled {
		t.Errorf("expected DISABLED, got %v", req.Status)
	}
}
