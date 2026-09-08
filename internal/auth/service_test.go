package auth

import (
	"os"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

func TestGenerateTokenPair(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	pair, err := svc.GenerateTokenPair("user-123", "tenant-456", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("expected access token to be non-empty")
	}
	if pair.RefreshToken == "" {
		t.Error("expected refresh token to be non-empty")
	}
	if pair.ExpiresAt == 0 {
		t.Error("expected expires_at to be non-zero")
	}
}

func TestValidateToken(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	pair, err := svc.GenerateTokenPair("user-123", "tenant-456", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("expected user_id user-123, got %s", claims.UserID)
	}
	if claims.TenantID != "tenant-456" {
		t.Errorf("expected tenant_id tenant-456, got %s", claims.TenantID)
	}
	if claims.Role != "user" {
		t.Errorf("expected role user, got %s", claims.Role)
	}
}

func TestValidateToken_InvalidSecret(t *testing.T) {
	cfg1 := &config.AuthConfig{
		JWTSecret:         "secret-1",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
	cfg2 := &config.AuthConfig{
		JWTSecret:         "secret-2",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc1 := NewService(cfg1, nil)
	svc2 := NewService(cfg2, nil)

	pair, err := svc1.GenerateTokenPair("user-123", "tenant-456", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc2.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error for invalid secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     -1 * time.Second,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	pair, err := svc.GenerateTokenPair("user-123", "tenant-456", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	_, err := svc.ValidateToken("invalid-token")
	if err == nil {
		t.Error("expected error for invalid token format")
	}
}

func TestGenerateTokenPair_NoSecret(t *testing.T) {
	os.Unsetenv("ARBITRAGE_JWT_SECRET")

	cfg := &config.AuthConfig{
		JWTSecret:         "",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	_, err := svc.GenerateTokenPair("user-123", "tenant-456", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateTokenPair_DefaultRole(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}

	svc := NewService(cfg, nil)

	pair, err := svc.GenerateTokenPair("user-123", "tenant-456", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.Role != "user" {
		t.Errorf("expected default role user, got %s", claims.Role)
	}
}

func TestHashToken(t *testing.T) {
	token := "test-refresh-token-12345"
	hash1 := hashToken(token)
	hash2 := hashToken(token)

	if hash1 != hash2 {
		t.Error("expected same hash for same token")
	}

	if hash1 == token {
		t.Error("hash should not equal original token")
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	token1, err := generateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	token2, err := generateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token1 == token2 {
		t.Error("expected different refresh tokens")
	}

	if len(token1) != 64 {
		t.Errorf("expected 64 char hex string, got %d chars", len(token1))
	}
}
