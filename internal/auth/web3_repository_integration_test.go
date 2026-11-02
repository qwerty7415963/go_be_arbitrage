//go:build integration

package auth

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupWalletIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
		t.Fatalf("failed to connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestGetUserByAddress_NullEmail(t *testing.T) {
	pool := setupWalletIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New()
	addr := "0x" + strings.Repeat("b", 40)
	chainID := int64(1)

	_, err := pool.Exec(ctx, "INSERT INTO users (id, auth_method, role, status) VALUES ($1, 'wallet', 'user', 'ACTIVE')", userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO wallet_addresses (id, user_id, address, chain_id, is_primary) VALUES ($1, $2, $3, $4, TRUE)", uuid.New(), userID, addr, chainID)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM wallet_addresses WHERE address = $1", addr)
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	repo := NewWeb3Repository(pool)
	user, err := repo.GetUserByAddress(ctx, addr, chainID)
	if err != nil {
		t.Fatalf("GetUserByAddress failed: %v", err)
	}
	if user == nil {
		t.Fatal("expected user")
	}
	if user.ID != userID {
		t.Errorf("id mismatch: got %s want %s", user.ID, userID)
	}
	if user.Email != "" {
		t.Errorf("expected empty email, got %q", user.Email)
	}
	if user.PasswordHash != "" {
		t.Errorf("expected empty password hash, got %q", user.PasswordHash)
	}
}

func TestGetUserByID_NullEmail(t *testing.T) {
	pool := setupWalletIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO users (id, auth_method, role, status) VALUES ($1, 'wallet', 'user', 'ACTIVE')", userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	repo := NewRepository(pool)
	user, err := repo.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if user == nil {
		t.Fatal("expected user")
	}
	if user.Email != "" {
		t.Errorf("expected empty email, got %q", user.Email)
	}
	if user.PasswordHash != "" {
		t.Errorf("expected empty password hash, got %q", user.PasswordHash)
	}
}

func signTestMessage(t *testing.T, msg string, priv *ecdsa.PrivateKey) string {
	t.Helper()
	hash := accounts.TextHash([]byte(msg))
	sig, err := crypto.Sign(hash, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return "0x" + hex.EncodeToString(sig)
}

func TestVerify_Idempotent(t *testing.T) {
	svc, _ := testWeb3ServiceDB(t)
	ctx := context.Background()

	priv, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	address := strings.ToLower(crypto.PubkeyToAddress(priv.PublicKey).Hex())

	// first
	nonce1, err := svc.GenerateNonce(ctx, address, 1)
	if err != nil {
		t.Fatalf("nonce1: %v", err)
	}
	msg1 := formatSIWEMessage(nonce1.Address, nonce1.ChainID, nonce1.Nonce, svc.config.SIWEDomain)
	sig1 := signTestMessage(t, msg1, priv)
	resp1, err := svc.VerifySignature(ctx, msg1, sig1)
	if err != nil {
		t.Fatalf("verify1: %v", err)
	}
	if resp1 == nil || resp1.User == nil {
		t.Fatal("expected response1")
	}

	// second
	nonce2, err := svc.GenerateNonce(ctx, address, 1)
	if err != nil {
		t.Fatalf("nonce2: %v", err)
	}
	msg2 := formatSIWEMessage(nonce2.Address, nonce2.ChainID, nonce2.Nonce, svc.config.SIWEDomain)
	sig2 := signTestMessage(t, msg2, priv)
	resp2, err := svc.VerifySignature(ctx, msg2, sig2)
	if err != nil {
		t.Fatalf("verify2: %v", err)
	}
	if resp2 == nil || resp2.User == nil {
		t.Fatal("expected response2")
	}
	if resp2.User.ID != resp1.User.ID {
		t.Errorf("expected same user id: %s vs %s", resp2.User.ID, resp1.User.ID)
	}
}
