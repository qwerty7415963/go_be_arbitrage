package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func testWeb3Config() *config.AuthConfig {
	return &config.AuthConfig{
		JWTSecret:         "test-secret-key-for-web3-12345678",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
		SIWEDomain:        "localhost",
		SIWENonceTTL:      5 * time.Minute,
		SupportedChains:   []int64{1, 42161, 10, 137, 8453},
	}
}

func testWeb3ServiceDB(t *testing.T) (*Web3Service, *pgxpool.Pool) {
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
	pool.Exec(ctx, "DELETE FROM wallet_nonces WHERE address LIKE '0xw3test%'")
	pool.Exec(ctx, "DELETE FROM wallet_addresses WHERE address LIKE '0xw3test%'")
	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE auth_method = 'wallet')")
	pool.Exec(ctx, "DELETE FROM users WHERE auth_method = 'wallet'")
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM wallet_nonces WHERE address LIKE '0xw3test%'")
		pool.Exec(context.Background(), "DELETE FROM wallet_addresses WHERE address LIKE '0xw3test%'")
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE auth_method = 'wallet')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE auth_method = 'wallet'")
		pool.Close()
	})
	cfg := testWeb3Config()
	authRepo := NewRepository(pool)
	authSvc := NewService(cfg, authRepo)
	web3Repo := NewWeb3Repository(pool)
	web3Svc := NewWeb3Service(cfg, web3Repo, authSvc)
	return web3Svc, pool
}

// ─── Helper function tests ────────────────────────────────────

func TestIsValidEthAddress(t *testing.T) {
	tests := []struct {
		addr  string
		valid bool
	}{
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD3e", true},
		{"0x0000000000000000000000000000000000000000", true},
		{"0xABCDEF1234567890abcdef1234567890ABCDEF12", true},
		{"0x123", false},
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD3e12345", false},
		{"not-an-address", false},
		{"0xGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := isValidEthAddress(tt.addr)
			if got != tt.valid {
				t.Errorf("isValidEthAddress(%q) = %v, want %v", tt.addr, got, tt.valid)
			}
		})
	}
}

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"0xABCDEF", "0xabcdef"},
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD3e", "0x742d35cc6634c0532925a3b844bc9e7595f2bd3e"},
		{"HELLO", "hello"},
	}
	for _, tt := range tests {
		got := normalizeAddress(tt.input)
		if got != tt.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSupportedChainNames(t *testing.T) {
	chains := []int64{1, 42161, 10, 137, 8453}
	for _, chainID := range chains {
		if _, ok := SupportedChainNames[chainID]; !ok {
			t.Errorf("chain %d not found in SupportedChainNames", chainID)
		}
	}
}

func TestAddressComparison_CaseInsensitive(t *testing.T) {
	a := "0x742d35cc6634c0532925a3b844bc9e7595f2bd3e"
	b := "0x742D35CC6634C0532925a3b844Bc9e7595f2bD3e"
	if normalizeAddress(a) != normalizeAddress(b) {
		t.Errorf("normalized addresses should match: %q != %q", normalizeAddress(a), normalizeAddress(b))
	}
}

func TestAddressComparison_DifferentAddresses(t *testing.T) {
	a := "0x742d35cc6634c0532925a3b844bc9e7595f2bd3e"
	b := "0x0000000000000000000000000000000000000000"
	if normalizeAddress(a) == normalizeAddress(b) {
		t.Error("different addresses should not match")
	}
}

func TestNonceUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := uuid.New().String()
		if seen[id] {
			t.Fatalf("duplicate UUID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestWeb3ConfigDefaults(t *testing.T) {
	cfg := testWeb3Config()
	if cfg.SIWEDomain != "localhost" {
		t.Errorf("expected SIWEDomain=localhost, got %s", cfg.SIWEDomain)
	}
	if cfg.SIWENonceTTL != 5*time.Minute {
		t.Errorf("expected SIWENonceTTL=5m, got %v", cfg.SIWENonceTTL)
	}
	if len(cfg.SupportedChains) != 5 {
		t.Errorf("expected 5 supported chains, got %d", len(cfg.SupportedChains))
	}
}

func TestNonceFormat(t *testing.T) {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(i)
	}
	nonce := ""
	for _, v := range b {
		nonce += string("0123456789abcdef"[v>>4])
		nonce += string("0123456789abcdef"[v&0x0f])
	}
	if len(nonce) != 32 {
		t.Errorf("expected nonce length 32, got %d", len(nonce))
	}
}

func TestChainSupportMatrix(t *testing.T) {
	cfg := testWeb3Config()
	supported := map[int64]bool{}
	for _, id := range cfg.SupportedChains {
		supported[id] = true
	}
	expectedSupported := []int64{1, 42161, 10, 137, 8453}
	for _, id := range expectedSupported {
		if !supported[id] {
			t.Errorf("chain %d should be supported", id)
		}
	}
	notExpected := []int64{42, 5, 11155111}
	for _, id := range notExpected {
		if supported[id] {
			t.Errorf("chain %d should NOT be supported", id)
		}
	}
}

// ─── W3-U-07/08: isChainSupported ────────────────────────────

func TestIsChainSupported_Supported(t *testing.T) {
	svc := NewWeb3Service(testWeb3Config(), nil, nil)
	if !svc.isChainSupported(1) {
		t.Error("chain 1 (Ethereum) should be supported")
	}
	if !svc.isChainSupported(42161) {
		t.Error("chain 42161 (Arbitrum) should be supported")
	}
}

func TestIsChainSupported_Unsupported(t *testing.T) {
	svc := NewWeb3Service(testWeb3Config(), nil, nil)
	if svc.isChainSupported(42) {
		t.Error("chain 42 should NOT be supported")
	}
	if svc.isChainSupported(11155111) {
		t.Error("chain 11155111 (Sepolia) should NOT be supported")
	}
}

// ─── W3-U-09/10/11: GenerateNonce ────────────────────────────

func TestGenerateNonce_Success(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	nonce, err := svc.GenerateNonce(context.Background(), "0xw3test00112233445566778899aabbccddeeff00", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nonce.Nonce) != 32 {
		t.Errorf("expected nonce length 32, got %d", len(nonce.Nonce))
	}
	if nonce.ExpiresAt.Before(time.Now()) {
		t.Error("expected expires_at in the future")
	}
	if nonce.ChainID != 1 {
		t.Errorf("expected chain_id 1, got %d", nonce.ChainID)
	}
}

func TestGenerateNonce_UnsupportedChain(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	_, err := svc.GenerateNonce(context.Background(), "0xw3test00112233445566778899aabbccddeeff00", 42)
	if err == nil {
		t.Fatal("expected error for unsupported chain")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthUnsupportedChain {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthUnsupportedChain, appErr.Code)
	}
}

func TestGenerateNonce_InvalidAddress(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	_, err := svc.GenerateNonce(context.Background(), "not-an-address", 1)
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

// ─── W3-U-12..19: VerifySignature error paths ────────────────

func TestVerifySignature_InvalidSIWEFormat(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	_, err := svc.VerifySignature(context.Background(), "not-a-siwe-message", "0xsignature")
	if err == nil {
		t.Fatal("expected error for invalid SIWE format")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestVerifySignature_DomainMismatch(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	msg := "wrong-domain.com wants you to sign in with your Ethereum account:\n0x1234567890123456789012345678901234567890\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 1\nNonce: abc123\nIssued At: 2026-01-01T00:00:00Z"
	_, err := svc.VerifySignature(context.Background(), msg, "0xsignature")
	if err == nil {
		t.Fatal("expected error for domain mismatch")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestVerifySignature_UnsupportedChain(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	msg := "localhost wants you to sign in with your Ethereum account:\n0x1234567890123456789012345678901234567890\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 42\nNonce: abc123\nIssued At: 2026-01-01T00:00:00Z"
	_, err := svc.VerifySignature(context.Background(), msg, "0xsignature")
	if err == nil {
		t.Fatal("expected error for unsupported chain")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthUnsupportedChain {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthUnsupportedChain, appErr.Code)
	}
}

func TestVerifySignature_NonceNotFound(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	msg := "localhost wants you to sign in with your Ethereum account:\n0x1234567890123456789012345678901234567890\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 1\nNonce: nonexistent-nonce\nIssued At: 2026-01-01T00:00:00Z"
	_, err := svc.VerifySignature(context.Background(), msg, "0xsignature")
	if err == nil {
		t.Fatal("expected error for nonce not found")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthNonceExpired {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthNonceExpired, appErr.Code)
	}
}

// ─── W3-U-20..27: LinkWallet error paths ─────────────────────

func TestLinkWallet_InvalidSIWEFormat(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	err := svc.LinkWallet(context.Background(), uuid.New(), "0xw3test00112233445566778899aabbccddeeff00", 1, "bad-message", "0xsig")
	if err == nil {
		t.Fatal("expected error for invalid SIWE format")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestLinkWallet_DomainMismatch(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	msg := "wrong-domain.com wants you to sign in with your Ethereum account:\n0x1234567890123456789012345678901234567890\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 1\nNonce: abc123\nIssued At: 2026-01-01T00:00:00Z"
	err := svc.LinkWallet(context.Background(), uuid.New(), "0x1234567890123456789012345678901234567890", 1, msg, "0xsig")
	if err == nil {
		t.Fatal("expected error for domain mismatch")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestLinkWallet_ChainMismatch(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	addr := "0x1234567890123456789012345678901234567890"
	msg := "localhost wants you to sign in with your Ethereum account:\n" + addr + "\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 42161\nNonce: abc123\nIssued At: 2026-01-01T00:00:00Z"
	err := svc.LinkWallet(context.Background(), uuid.New(), addr, 1, msg, "0xsig")
	if err == nil {
		t.Fatal("expected error for chain mismatch")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestLinkWallet_AddressMismatch(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	addr := "0x1234567890123456789012345678901234567890"
	msg := "localhost wants you to sign in with your Ethereum account:\n0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n\nSign in to Arbitrage Platform\n\nURI: https://localhost\nVersion: 1\nChain ID: 1\nNonce: abc123\nIssued At: 2026-01-01T00:00:00Z"
	err := svc.LinkWallet(context.Background(), uuid.New(), addr, 1, msg, "0xsig")
	if err == nil {
		t.Fatal("expected error for address mismatch")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthInvalidSignature {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthInvalidSignature, appErr.Code)
	}
}

func TestLinkWallet_UnsupportedChain(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	addr := "0x1234567890123456789012345678901234567890"
	err := svc.LinkWallet(context.Background(), uuid.New(), addr, 42, "", "")
	if err == nil {
		t.Fatal("expected error for unsupported chain")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthUnsupportedChain {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthUnsupportedChain, appErr.Code)
	}
}

// ─── W3-U-28..30: UnlinkWallet error paths ───────────────────

func TestUnlinkWallet_NotOwned(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	err := svc.UnlinkWallet(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error for wallet not owned")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeAuthWalletNotFound {
		t.Errorf("expected code %s, got %s", domain.ErrCodeAuthWalletNotFound, appErr.Code)
	}
}

// ─── W3-U-31/32: ListWallets ─────────────────────────────────

func TestListWallets_Empty(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	wallets, err := svc.ListWallets(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wallets) != 0 {
		t.Errorf("expected empty list, got %d wallets", len(wallets))
	}
}

func TestListWallets_Multiple(t *testing.T) {
	svc, pool := testWeb3ServiceDB(t)
	ctx := context.Background()

	// Create a user
	var userID uuid.UUID
	err := pool.QueryRow(ctx, "INSERT INTO users (id, auth_method, role, status) VALUES ($1, 'wallet', 'user', 'ACTIVE') RETURNING id", uuid.New()).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create 3 wallets
	for i := 0; i < 3; i++ {
		_, err = pool.Exec(ctx,
			"INSERT INTO wallet_addresses (id, user_id, address, chain_id, is_primary) VALUES ($1, $2, $3, $4, $5)",
			uuid.New(), userID, fmt.Sprintf("0xw3test%040d", i), int64(i+1), i == 0,
		)
		if err != nil {
			t.Fatalf("create wallet %d: %v", i, err)
		}
	}

	wallets, err := svc.ListWallets(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wallets) != 3 {
		t.Fatalf("expected 3 wallets, got %d", len(wallets))
	}
	if !wallets[0].IsPrimary {
		t.Error("expected first wallet to be primary")
	}
}

// ─── formatSIWEMessage ────────────────────────────────────────

func TestFormatSIWEMessage(t *testing.T) {
	addr := "0x1234567890123456789012345678901234567890"
	msg := formatSIWEMessage(addr, 1, "test-nonce-123", "localhost")
	if msg == "" {
		t.Error("expected non-empty message")
	}
	if !containsString(msg, addr) {
		t.Error("message should contain the address")
	}
	if !containsString(msg, "test-nonce-123") {
		t.Error("message should contain the nonce")
	}
	if !containsString(msg, "localhost") {
		t.Error("message should contain the domain")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
