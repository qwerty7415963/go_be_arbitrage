//go:build integration

package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupIntegrationDB(t *testing.T) *pgxpool.Pool {
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

	// Clean test data
	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%repo-test%')")
	pool.Exec(ctx, "DELETE FROM users WHERE email LIKE '%repo-test%'")

	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%repo-test%')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE email LIKE '%repo-test%'")
		pool.Close()
	})

	return pool
}

func testUser() *User {
	return &User{
		ID:           uuid.New(),
		Email:        fmt.Sprintf("repo-test-%d@test.com", time.Now().UnixNano()),
		PasswordHash: "$2a$10$fakehashforspeed",
		Role:         "user",
		Status:       "ACTIVE",
	}
}

// ─── AUTH-I-01: CreateUser → GetByEmail → GetByID ─────────────

func TestRepository_CreateAndGet(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()

	err := repo.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	// GetByEmail
	found, err := repo.GetUserByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}

	if found.ID != user.ID {
		t.Errorf("expected ID %s, got %s", user.ID, found.ID)
	}
	if found.Email != user.Email {
		t.Errorf("expected email %s, got %s", user.Email, found.Email)
	}
	if found.Role != "user" {
		t.Errorf("expected role user, got %s", found.Role)
	}

	// GetByID
	found2, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if found2.Email != user.Email {
		t.Errorf("expected same email, got %s", found2.Email)
	}
}

// ─── AUTH-I-02: Duplicate email unique constraint ─────────────

func TestRepository_DuplicateEmail(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user1 := testUser()
	user2 := testUser()
	user2.Email = user1.Email // same email

	if err := repo.CreateUser(ctx, user1); err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	err := repo.CreateUser(ctx, user2)
	if err == nil {
		t.Error("expected error for duplicate email, got nil")
	}
}

// ─── AUTH-I-03: ExistsByEmail ─────────────────────────────────

func TestRepository_ExistsByEmail(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()

	exists, err := repo.ExistsByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("ExistsByEmail failed: %v", err)
	}
	if exists {
		t.Error("expected not exists before create")
	}

	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	exists, err = repo.ExistsByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("ExistsByEmail failed: %v", err)
	}
	if !exists {
		t.Error("expected exists after create")
	}
}

// ─── AUTH-I-04: GetUserByEmail not found ──────────────────────

func TestRepository_GetUserByEmail_NotFound(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	_, err := repo.GetUserByEmail(ctx, "nonexistent@test.com")
	if err == nil {
		t.Error("expected error for non-existent email")
	}
}

// ─── AUTH-I-05: CreateRefreshToken + GetByHash ────────────────

func TestRepository_RefreshToken_Lifecycle(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	token := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: fmt.Sprintf("hash-%d", time.Now().UnixNano()),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Revoked:   false,
	}

	// Create
	if err := repo.CreateRefreshToken(ctx, token); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}
	if token.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	// GetByHash
	found, err := repo.GetRefreshTokenByHash(ctx, token.TokenHash)
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash failed: %v", err)
	}
	if found.ID != token.ID {
		t.Errorf("expected ID %s, got %s", token.ID, found.ID)
	}
	if found.Revoked {
		t.Error("expected revoked=false")
	}
}

// ─── AUTH-I-06: RevokeRefreshToken ────────────────────────────

func TestRepository_RevokeRefreshToken(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	token := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: fmt.Sprintf("revoke-hash-%d", time.Now().UnixNano()),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Revoked:   false,
	}

	if err := repo.CreateRefreshToken(ctx, token); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	if err := repo.RevokeRefreshToken(ctx, token.TokenHash); err != nil {
		t.Fatalf("RevokeRefreshToken failed: %v", err)
	}

	found, _ := repo.GetRefreshTokenByHash(ctx, token.TokenHash)
	if !found.Revoked {
		t.Error("expected revoked=true after revoke")
	}
}

// ─── AUTH-I-07: RevokeAllUserRefreshTokens ────────────────────

func TestRepository_RevokeAllUserRefreshTokens(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create multiple tokens
	for i := 0; i < 3; i++ {
		token := &RefreshToken{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: fmt.Sprintf("all-hash-%d-%d", i, time.Now().UnixNano()),
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			Revoked:   false,
		}
		if err := repo.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("CreateRefreshToken failed: %v", err)
		}
	}

	// Revoke all
	if err := repo.RevokeAllUserRefreshTokens(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAllUserRefreshTokens failed: %v", err)
	}

	// Verify all are revoked
	var count int
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND revoked = FALSE",
		user.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 non-revoked tokens, got %d", count)
	}
}

// ─── AUTH-I-08: DeleteExpiredRefreshTokens ────────────────────

func TestRepository_DeleteExpiredRefreshTokens(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create an expired token
	expiredToken := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: fmt.Sprintf("expired-hash-%d", time.Now().UnixNano()),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // expired
		Revoked:   false,
	}
	if err := repo.CreateRefreshToken(ctx, expiredToken); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	// Create a valid token
	validToken := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: fmt.Sprintf("valid-hash-%d", time.Now().UnixNano()),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Revoked:   false,
	}
	if err := repo.CreateRefreshToken(ctx, validToken); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	deleted, err := repo.DeleteExpiredRefreshTokens(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredRefreshTokens failed: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", deleted)
	}

	// Valid token should still exist
	_, err = repo.GetRefreshTokenByHash(ctx, validToken.TokenHash)
	if err != nil {
		t.Errorf("valid token should still exist: %v", err)
	}
}

// ─── AUTH-I-09: UpdateLastLogin ───────────────────────────────

func TestRepository_UpdateLastLogin(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.LastLoginAt != nil {
		t.Error("expected LastLoginAt to be nil before update")
	}

	if err := repo.UpdateLastLogin(ctx, user.ID); err != nil {
		t.Fatalf("UpdateLastLogin failed: %v", err)
	}

	found, _ := repo.GetUserByID(ctx, user.ID)
	if found.LastLoginAt == nil {
		t.Error("expected LastLoginAt to be set after update")
	}
}

// ─── AUTH-I-10: UpdatePasswordHash ────────────────────────────

func TestRepository_UpdatePasswordHash(t *testing.T) {
	pool := setupIntegrationDB(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	user := testUser()
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	originalHash := user.PasswordHash
	newHash := "$2a$10$newhashvalue1234567890123456789012345678"

	if err := repo.UpdatePasswordHash(ctx, user.ID, newHash); err != nil {
		t.Fatalf("UpdatePasswordHash failed: %v", err)
	}

	found, _ := repo.GetUserByID(ctx, user.ID)
	if found.PasswordHash == originalHash {
		t.Error("expected password hash to be updated")
	}
	if found.PasswordHash != newHash {
		t.Errorf("expected new hash, got %s", found.PasswordHash)
	}
}
