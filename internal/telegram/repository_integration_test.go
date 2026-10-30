//go:build integration

package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func TestRepo_BindingLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userID := testUserTG(t, pool)

	chatID := int64(12345)
	if err := repo.UpsertBinding(ctx, &Binding{UserID: userID, ChatID: chatID, Locale: LocaleVI, Status: BindingStatusActive}); err != nil {
		t.Fatal(err)
	}
	b, err := repo.GetBinding(ctx, userID)
	if err != nil || b == nil || b.ChatID != chatID {
		t.Fatalf("get: %v %+v", err, b)
	}
	// mark blocked
	if err := repo.MarkBlocked(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	b2, _ := repo.GetBinding(ctx, userID)
	if b2.Status != BindingStatusBlocked {
		t.Fatalf("status %s", b2.Status)
	}
	if err := repo.DeleteBinding(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetBinding(ctx, userID); err != pgx.ErrNoRows {
		t.Fatalf("expected no rows, got %v", err)
	}
}

func TestRepo_ConsumeCode_SingleUseAndExpired(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userID := testUserTG(t, pool)

	code, _ := GenerateCode()
	if err := repo.CreateCode(ctx, &LinkCode{Code: code, UserID: userID, Locale: LocaleVI, ExpiresAt: time.Now().Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	lc, err := repo.ConsumeCode(ctx, code)
	if err != nil || lc.UserID != userID {
		t.Fatalf("consume: %v", err)
	}
	if _, err := repo.ConsumeCode(ctx, code); err == nil {
		t.Fatal("expected second consume fail")
	}

	expired, _ := GenerateCode()
	if err := repo.CreateCode(ctx, &LinkCode{Code: expired, UserID: userID, Locale: LocaleVI, ExpiresAt: time.Now().Add(-1 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumeCode(ctx, expired); err == nil {
		t.Fatal("expected expired fail")
	}
	_ = errors.New
	_ = domain.ErrCodeValidation
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func testUserTG(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenantID, userID := uuid.New(), uuid.New()
	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, fmt.Sprintf("tg-%d", run)); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'x')`, userID, tenantID, fmt.Sprintf("tg-%d@test.com", run)); err != nil {
		t.Fatalf("user: %v", err)
	}
	return tenantID, userID
}
