//go:build integration

package notify

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
)

func TestDedup_Integration(t *testing.T) {
	ctx := context.Background()
	pool := testPoolNotify(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userID := testUserNotify(t, pool)

	chatID := int64(999)

	bindingRepo := telegram.NewRepository(pool)
	_ = bindingRepo.UpsertBinding(ctx, &telegram.Binding{UserID: userID, ChatID: chatID, Locale: telegram.LocaleVI, Status: telegram.BindingStatusActive})

	key := fmt.Sprintf("user:%s:fill:12345", userID)
	row := &OutboxRow{UserID: userID, ChatID: chatID, Body: "hello", Attempts: 0}
	k := key
	row.DedupKey = &k
	inserted, err := repo.InsertOutbox(ctx, row)
	if err != nil || !inserted {
		t.Fatalf("first insert: %v %v", err, inserted)
	}
	row2 := &OutboxRow{UserID: userID, ChatID: chatID, Body: "hello2", Attempts: 0}
	row2.DedupKey = &k
	inserted2, err2 := repo.InsertOutbox(ctx, row2)
	if err2 != nil {
		t.Fatalf("second insert err: %v", err2)
	}
	if inserted2 {
		t.Fatal("expected dedup to block second insert")
	}
}

func testPoolNotify(t *testing.T) *pgxpool.Pool {
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

func testUserNotify(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
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
