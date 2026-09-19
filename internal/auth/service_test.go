package auth

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func testServiceDB(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: requires PostgreSQL (run without -short)")
	}
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn := fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping test DB: %v", err)
	}
	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'svc-test-%')")
	pool.Exec(ctx, "DELETE FROM users WHERE email LIKE 'svc-test-%'")
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'svc-test-%')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE email LIKE 'svc-test-%'")
		pool.Close()
	})
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret-key-for-unit",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
	repo := NewRepository(pool)
	svc := NewService(cfg, repo)
	return svc, pool
}

func defaultTestCfg() *config.AuthConfig {
	return &config.AuthConfig{
		JWTSecret:         "test-secret-key-for-unit",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
}

// ─── AUTH-U-01: Register success ──────────────────────────────

func TestRegister_Success(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-%d@register.com", time.Now().UnixNano())
	req := &RegisterRequest{Email: email, Password: "testpass123"}
	resp, err := svc.Register(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh_token")
	}
	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != email {
		t.Errorf("expected email %s, got %s", email, resp.User.Email)
	}
	if resp.User.Role != "user" {
		t.Errorf("expected role user, got %s", resp.User.Role)
	}
	if resp.User.Status != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %s", resp.User.Status)
	}
}

// ─── AUTH-U-02: Register duplicate email ──────────────────────

func TestRegister_DuplicateEmail(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-dup-%d@register.com", time.Now().UnixNano())
	req := &RegisterRequest{Email: email, Password: "testpass123"}
	_, err := svc.Register(context.Background(), req)
	if err != nil {
		t.Fatalf("first register: unexpected error: %v", err)
	}
	_, err = svc.Register(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeConflict {
		t.Errorf("expected code %s, got %s", domain.ErrCodeConflict, appErr.Code)
	}
}

// ─── AUTH-U-03: Register invalid email format ─────────────────

func TestRegister_InvalidEmailFormat(t *testing.T) {
	svc, _ := testServiceDB(t)
	req := &RegisterRequest{Email: "not-an-email", Password: "testpass123"}
	_, err := svc.Register(context.Background(), req)
	if err == nil {
		t.Error("expected error for invalid email")
	}
}

// ─── AUTH-U-04: Register short password ───────────────────────

func TestRegister_ShortPassword(t *testing.T) {
	svc, _ := testServiceDB(t)
	req := &RegisterRequest{Email: "svc-test-short@test.com", Password: "ab"}
	_, err := svc.Register(context.Background(), req)
	if err == nil {
		t.Error("expected error for short password")
	}
}

// ─── AUTH-U-05: Login success ─────────────────────────────────

func TestLogin_Success(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-login-%d@test.com", time.Now().UnixNano())
	password := "testpass123"
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: password})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	resp, err := svc.Login(context.Background(), &LoginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh_token")
	}
	if resp.User == nil {
		t.Fatal("expected user")
	}
	if resp.User.Email != email {
		t.Errorf("expected email %s, got %s", email, resp.User.Email)
	}
}

// ─── AUTH-U-06: Login nonexistent email ───────────────────────

func TestLogin_NonExistentEmail(t *testing.T) {
	svc, _ := testServiceDB(t)
	_, err := svc.Login(context.Background(), &LoginRequest{Email: "svc-test-nonexistent@test.com", Password: "whatever"})
	if err == nil {
		t.Fatal("expected error for nonexistent email")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidCredentials {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidCredentials, appErr.Code)
	}
}

// ─── AUTH-U-07: Login wrong password ──────────────────────────

func TestLogin_WrongPassword(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-wrongpw-%d@test.com", time.Now().UnixNano())
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "correctpass"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	_, err = svc.Login(context.Background(), &LoginRequest{Email: email, Password: "wrongpass"})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidCredentials {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidCredentials, appErr.Code)
	}
}

// ─── AUTH-U-08: Login disabled user ───────────────────────────

func TestLogin_DisabledUser(t *testing.T) {
	svc, pool := testServiceDB(t)
	email := fmt.Sprintf("svc-test-disabled-%d@test.com", time.Now().UnixNano())
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	pool.Exec(context.Background(), "UPDATE users SET status = 'DISABLED' WHERE email = $1", email)
	_, err = svc.Login(context.Background(), &LoginRequest{Email: email, Password: "testpass123"})
	if err == nil {
		t.Fatal("expected error for disabled user")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthDisabled {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthDisabled, appErr.Code)
	}
}

// ─── AUTH-U-09: Refresh success ───────────────────────────────

func TestRefresh_Success(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-refresh-%d@test.com", time.Now().UnixNano())
	regResp, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	resp, err := svc.Refresh(context.Background(), regResp.RefreshToken)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected new access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected new refresh_token")
	}
}

// ─── AUTH-U-10: Refresh token not found ───────────────────────

func TestRefresh_TokenNotFound(t *testing.T) {
	svc, _ := testServiceDB(t)
	_, err := svc.Refresh(context.Background(), "nonexistent-token-abc123")
	if err == nil {
		t.Fatal("expected error for nonexistent refresh token")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthTokenInvalid {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthTokenInvalid, appErr.Code)
	}
}

// ─── AUTH-U-11: Refresh revoked token ─────────────────────────

func TestRefresh_RevokedToken(t *testing.T) {
	svc, pool := testServiceDB(t)
	email := fmt.Sprintf("svc-test-revoked-%d@test.com", time.Now().UnixNano())
	regResp, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	var userID uuid.UUID
	pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
	pool.Exec(context.Background(), "UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1", userID)
	_, err = svc.Refresh(context.Background(), regResp.RefreshToken)
	if err == nil {
		t.Fatal("expected error for revoked refresh token")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthRefreshRevoked {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthRefreshRevoked, appErr.Code)
	}
}

// ─── AUTH-U-12: Refresh expired token ─────────────────────────

func TestRefresh_ExpiredToken(t *testing.T) {
	svc, pool := testServiceDB(t)
	email := fmt.Sprintf("svc-test-expired-%d@test.com", time.Now().UnixNano())
	regResp, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	tokenHash := hashToken(regResp.RefreshToken)
	pool.Exec(context.Background(), "UPDATE refresh_tokens SET expires_at = NOW() - INTERVAL '1 hour' WHERE token_hash = $1", tokenHash)
	_, err = svc.Refresh(context.Background(), regResp.RefreshToken)
	if err == nil {
		t.Fatal("expected error for expired refresh token")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthTokenExpired {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthTokenExpired, appErr.Code)
	}
}

// ─── AUTH-U-13: Logout revokes all tokens ─────────────────────

func TestLogout_RevokesAllTokens(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-logout-%d@test.com", time.Now().UnixNano())
	regResp, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	claims, err := svc.ValidateToken(regResp.AccessToken)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	userID, _ := uuid.Parse(claims.UserID)
	err = svc.Logout(context.Background(), userID)
	if err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	_, err = svc.Refresh(context.Background(), regResp.RefreshToken)
	if err == nil {
		t.Fatal("expected error after logout")
	}
}

// ─── AUTH-U-14: ChangePassword success ────────────────────────

func TestChangePassword_Success(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-changepw-%d@test.com", time.Now().UnixNano())
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "oldpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	var userID uuid.UUID
	svc.repo.db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
	err = svc.ChangePassword(context.Background(), userID, "oldpass123", "newpass456")
	if err != nil {
		t.Fatalf("change password failed: %v", err)
	}
	_, err = svc.Login(context.Background(), &LoginRequest{Email: email, Password: "oldpass123"})
	if err == nil {
		t.Error("old password should fail after change")
	}
	resp, err := svc.Login(context.Background(), &LoginRequest{Email: email, Password: "newpass456"})
	if err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token with new password")
	}
}

// ─── AUTH-U-15: ChangePassword wrong old password ─────────────

func TestChangePassword_WrongOldPassword(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-changepw-wrong-%d@test.com", time.Now().UnixNano())
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "correctpass"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	var userID uuid.UUID
	svc.repo.db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
	err = svc.ChangePassword(context.Background(), userID, "wrongold", "newpass123")
	if err == nil {
		t.Fatal("expected error for wrong old password")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidCredentials {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidCredentials, appErr.Code)
	}
}

// ─── AUTH-U-16: ChangePassword short new password ─────────────

func TestChangePassword_ShortNewPassword(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-changepw-short-%d@test.com", time.Now().UnixNano())
	_, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	var userID uuid.UUID
	svc.repo.db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
	err = svc.ChangePassword(context.Background(), userID, "testpass123", "ab")
	if err == nil {
		t.Fatal("expected error for short new password")
	}
}

// ─── AUTH-U-17: GetUser success ───────────────────────────────

func TestGetUser_Success(t *testing.T) {
	svc, _ := testServiceDB(t)
	email := fmt.Sprintf("svc-test-getuser-%d@test.com", time.Now().UnixNano())
	regResp, err := svc.Register(context.Background(), &RegisterRequest{Email: email, Password: "testpass123"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	claims, _ := svc.ValidateToken(regResp.AccessToken)
	userID, _ := uuid.Parse(claims.UserID)
	user, err := svc.GetUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user failed: %v", err)
	}
	if user.Email != email {
		t.Errorf("expected email %s, got %s", email, user.Email)
	}
}

// ─── AUTH-U-18: GetUser not found ─────────────────────────────

func TestGetUser_NotFound(t *testing.T) {
	svc, _ := testServiceDB(t)
	_, err := svc.GetUser(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

// ─── AUTH-U-19/20: EnsureAdmin skips ──────────────────────────

func TestEnsureAdmin_SkipsWhenEmailEmpty(t *testing.T) {
	svc, _ := testServiceDB(t)
	err := svc.EnsureAdmin(context.Background(), "", "password")
	if err != nil {
		t.Errorf("expected nil error when email empty, got %v", err)
	}
}

func TestEnsureAdmin_SkipsWhenPasswordEmpty(t *testing.T) {
	svc, _ := testServiceDB(t)
	err := svc.EnsureAdmin(context.Background(), "admin@test.com", "")
	if err != nil {
		t.Errorf("expected nil error when password empty, got %v", err)
	}
}

// ─── Token operations (no DB needed) ──────────────────────────

func TestValidateToken_CorrectClaims(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, err := svc.GenerateTokenPair("user-uuid-123", "tenant-uuid-456", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != "user-uuid-123" {
		t.Errorf("expected user_id user-uuid-123, got %s", claims.UserID)
	}
	if claims.TenantID != "tenant-uuid-456" {
		t.Errorf("expected tenant_id tenant-uuid-456, got %s", claims.TenantID)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role admin, got %s", claims.Role)
	}
	if claims.Issuer != "arbitrage-platform" {
		t.Errorf("expected issuer arbitrage-platform, got %s", claims.Issuer)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	svc1 := NewService(defaultTestCfg(), nil)
	svc2 := NewService(&config.AuthConfig{
		JWTSecret:         "completely-different-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}, nil)
	pair, _ := svc1.GenerateTokenPair("u1", "t1", "user")
	_, err := svc2.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error for wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret:         "test-secret",
		JWTExpiration:     -1 * time.Second,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
	svc := NewService(cfg, nil)
	pair, _ := svc.GenerateTokenPair("u1", "t1", "user")
	_, err := svc.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	_, err := svc.ValidateToken("not-a-jwt")
	if err == nil {
		t.Error("expected error for invalid token format")
	}
}

func TestValidateToken_EmptyString(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	_, err := svc.ValidateToken("")
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidateToken_TamperedPayload(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, _ := svc.GenerateTokenPair("u1", "t1", "user")
	tampered := pair.AccessToken[:len(pair.AccessToken)/2] + "X" + pair.AccessToken[len(pair.AccessToken)/2+1:]
	_, err := svc.ValidateToken(tampered)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestGenerateTokenPair_AdminRole(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, err := svc.GenerateTokenPair("u1", "t1", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claims, _ := svc.ValidateToken(pair.AccessToken)
	if claims.Role != "admin" {
		t.Errorf("expected admin, got %s", claims.Role)
	}
}

func TestGenerateTokenPair_EmptyRole(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, err := svc.GenerateTokenPair("u1", "t1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claims, _ := svc.ValidateToken(pair.AccessToken)
	if claims.Role != "user" {
		t.Errorf("expected default role user, got %s", claims.Role)
	}
}

func TestGenerateTokenPair_ExpiresAtInFuture(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, err := svc.GenerateTokenPair("u1", "t1", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.ExpiresAt <= time.Now().Unix() {
		t.Error("expected expires_at in the future")
	}
	futureLimit := time.Now().Add(16 * time.Minute).Unix()
	if pair.ExpiresAt > futureLimit {
		t.Errorf("expires_at too far in the future: %d", pair.ExpiresAt)
	}
}

func TestGenerateTokenPair_TokensAreDifferent(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair1, _ := svc.GenerateTokenPair("user-A", "t1", "user")
	pair2, _ := svc.GenerateTokenPair("user-B", "t1", "user")
	if pair1.AccessToken == pair2.AccessToken {
		t.Error("expected different access tokens for different users")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("abc123")
	h2 := hashToken("abc123")
	if h1 != h2 {
		t.Error("expected same hash for same input")
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	h1 := hashToken("abc")
	h2 := hashToken("xyz")
	if h1 == h2 {
		t.Error("expected different hashes for different inputs")
	}
}

func TestHashToken_Length(t *testing.T) {
	h := hashToken("anything")
	if len(h) != 64 {
		t.Errorf("expected 64 char hex, got %d", len(h))
	}
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	t1, _ := generateRefreshToken()
	t2, _ := generateRefreshToken()
	if t1 == t2 {
		t.Error("expected different refresh tokens")
	}
}

func TestGenerateRefreshToken_Length(t *testing.T) {
	tok, _ := generateRefreshToken()
	if len(tok) != 64 {
		t.Errorf("expected 64 char hex, got %d", len(tok))
	}
}

func TestValidateToken_Concurrent(t *testing.T) {
	svc := NewService(defaultTestCfg(), nil)
	pair, _ := svc.GenerateTokenPair("u1", "t1", "user")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ValidateToken(pair.AccessToken)
			if err != nil {
				t.Errorf("concurrent validation failed: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestHashToken_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h := hashToken(fmt.Sprintf("token-%d", n))
			if len(h) != 64 {
				t.Errorf("unexpected hash length: %d", len(h))
			}
		}(i)
	}
	wg.Wait()
}
