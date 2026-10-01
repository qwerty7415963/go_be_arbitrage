//go:build integration

package tradergroup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

var addrSeq atomic.Uint64

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
	pool, err := pgxpool.New(ctx,
		fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func testUser(t *testing.T, pool *pgxpool.Pool) (tenantID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenantID, userID = uuid.New(), uuid.New()
	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		tenantID, fmt.Sprintf("tg-%d", run)); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'x')`,
		userID, tenantID, fmt.Sprintf("tg-%d-%d@test.com", run, addrSeq.Add(1))); err != nil {
		t.Fatalf("user: %v", err)
	}
	return tenantID, userID
}

func testVenueID(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('trader-test-venue', 'Trader Test Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue: %v", err)
	}
	tr := trader.NewRepository(pool)
	id, err := tr.VenueIDByCode(context.Background(), "trader-test-venue")
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	return id
}

func seedRegistry(t *testing.T, pool *pgxpool.Pool, venueID uuid.UUID, n int) []string {
	t.Helper()
	tr := trader.NewRepository(pool)
	addrs := make([]string, n)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("0x%040x", uint64(time.Now().UnixNano())%0xffff+addrSeq.Add(1)*0x10000)
		if _, _, err := tr.UpsertRegistry(context.Background(), venueID, addrs[i],
			trader.SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, a := range addrs {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, venueID, a)
		}
	})
	return addrs
}

func mustCode(t *testing.T, err error) domain.ErrorCode {
	t.Helper()
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T (%v)", err, err)
	}
	return appErr.Code
}

// GRP: full lifecycle — create, duplicate 409, list, update, members with
// alias/note, idempotent re-add (BE-029), remove, delete keeps registry (BE-030).
func TestRepo_GroupLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 2)

	g, err := repo.Create(ctx, userA, "Alphas", "top HL wallets")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID) })

	if _, err := repo.Create(ctx, userA, "Alphas", ""); mustCode(t, err) != domain.ErrCodeGroupDuplicate {
		t.Errorf("duplicate name: want GROUP-002, got %v", err)
	}

	added, err := repo.AddMembers(ctx, g.ID, userA, []MemberInput{
		{Venue: "trader-test-venue", WalletAddress: addrs[0], Alias: "whale-1", Note: "watch"},
		{Venue: "trader-test-venue", WalletAddress: addrs[1]},
	})
	if err != nil || added != 2 {
		t.Fatalf("add: %v added=%d", err, added)
	}
	// Idempotent re-add keeps alias/note (BE-029).
	added, err = repo.AddMembers(ctx, g.ID, userA, []MemberInput{
		{Venue: "trader-test-venue", WalletAddress: addrs[0], Alias: "CHANGED", Note: "CHANGED"},
	})
	if err != nil || added != 0 {
		t.Fatalf("re-add: %v added=%d", err, added)
	}
	members, err := repo.ListMembers(ctx, g.ID, userA)
	if err != nil || len(members) != 2 {
		t.Fatalf("members: %v n=%d", err, len(members))
	}
	if members[0].Alias != "whale-1" || members[0].Note != "watch" {
		t.Errorf("alias/note lost: %+v", members[0])
	}

	got, err := repo.Get(ctx, g.ID, userA)
	if err != nil || got.MemberCount != 2 {
		t.Fatalf("get: %v %+v", err, got)
	}
	list, err := repo.List(ctx, userA)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v n=%d", err, len(list))
	}

	rename := "Betas"
	updated, err := repo.Update(ctx, g.ID, userA, &rename, nil)
	if err != nil || updated.Name != "Betas" {
		t.Fatalf("update: %v %+v", err, updated)
	}

	removed, err := repo.RemoveMembers(ctx, g.ID, userA, venueID, []string{addrs[1]})
	if err != nil || removed != 1 {
		t.Fatalf("remove: %v removed=%d", err, removed)
	}
	removed, err = repo.RemoveMembers(ctx, g.ID, userA, venueID, []string{addrs[1]})
	if err != nil || removed != 0 {
		t.Fatalf("re-remove must be no-op: %v removed=%d", err, removed)
	}

	if err := repo.Delete(ctx, g.ID, userA); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Registry rows survive the group delete (BE-030).
	tr := trader.NewRepository(pool)
	if _, err := tr.GetRegistry(ctx, venueID, addrs[0]); err != nil {
		t.Errorf("registry row must survive group delete: %v", err)
	}
	if _, err := repo.Get(ctx, g.ID, userA); mustCode(t, err) != domain.ErrCodeNotFound {
		t.Errorf("deleted group: want 404, got %v", err)
	}
}

// GRP-H-03/BE-028: tenant isolation on every owner-scoped path.
func TestRepo_GroupIsolation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(pool.Close)
	repo := NewRepository(pool)
	_, userA := testUser(t, pool)
	_, userB := testUser(t, pool)
	venueID := testVenueID(t, pool)
	addrs := seedRegistry(t, pool, venueID, 1)

	g, err := repo.Create(ctx, userA, "Private", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, g.ID) })

	for name, fn := range map[string]func() error{
		"get":    func() error { _, err := repo.Get(ctx, g.ID, userB); return err },
		"update": func() error { _, err := repo.Update(ctx, g.ID, userB, nil, nil); return err },
		"delete": func() error { return repo.Delete(ctx, g.ID, userB) },
		"add": func() error {
			_, err := repo.AddMembers(ctx, g.ID, userB,
				[]MemberInput{{Venue: "trader-test-venue", WalletAddress: addrs[0]}})
			return err
		},
		"members": func() error { _, err := repo.ListMembers(ctx, g.ID, userB); return err },
		"remove": func() error {
			_, err := repo.RemoveMembers(ctx, g.ID, userB, venueID, addrs)
			return err
		},
	} {
		if code := mustCode(t, fn()); code != domain.ErrCodeGroupForbidden {
			t.Errorf("%s: want GROUP-003, got %s", name, code)
		}
	}
	if _, err := repo.Get(ctx, uuid.New(), userA); mustCode(t, err) != domain.ErrCodeNotFound {
		t.Errorf("unknown group: want 404, got %v", err)
	}
	if _, err := repo.OwnerOf(ctx, g.ID); err != nil {
		t.Errorf("owner: %v", err)
	}
}
