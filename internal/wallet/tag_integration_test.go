//go:build integration

package wallet

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

func tagFixtureWallet(t *testing.T, f *fixture, i int) (uuid.UUID, string) {
	t.Helper()
	addr := fmt.Sprintf("0x%040x", 0x7000+i)
	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO tracked_wallets (chain, address)
		VALUES ('evm', $1)
		ON CONFLICT (chain, address) DO UPDATE SET last_seen_at = NOW()
		RETURNING id`, addr).Scan(&id)
	if err != nil {
		t.Fatalf("add wallet: %v", err)
	}
	f.addrs = append(f.addrs, addr)
	return id, addr
}

func tagFixtureUser(t *testing.T, f *fixture, i int) uuid.UUID {
	t.Helper()
	uid := uuid.New()
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'test-hash')`,
		uid, f.tenantID, fmt.Sprintf("tag-fixture-%d-%d@test.com", time.Now().UnixNano(), i))
	if err != nil {
		t.Fatalf("add user: %v", err)
	}
	return uid
}

// TAG-I-01: tag persists and is strictly per-user — B sees null and B's
// tag search never matches A's label.
func TestRepo_Tag_PerUserIsolation(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	userB := tagFixtureUser(t, f, 1)
	t.Cleanup(func() {
		// Runs before the fixture cleanup closes the pool.
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userB)
	})
	walletID, addr := tagFixtureWallet(t, f, 1)

	detail, err := f.svc.UpdateTag(ctx, f.userA, walletID, strPtr("  A label  "))
	if err != nil {
		t.Fatalf("update tag: %v", err)
	}
	if detail.Tag == nil || *detail.Tag != "A label" {
		t.Errorf("trimmed tag not returned: %v", detail.Tag)
	}

	// Caller-scoped read: A sees it, B sees null.
	gotA, err := f.svc.Detail(ctx, f.userA, walletID, url.Values{})
	if err != nil || gotA.Tag == nil || *gotA.Tag != "A label" {
		t.Errorf("A detail: %v %v", gotA.Tag, err)
	}
	gotB, err := f.svc.Detail(ctx, userB, walletID, url.Values{})
	if err != nil {
		t.Fatalf("B detail: %v", err)
	}
	if gotB.Tag != nil {
		t.Errorf("B must not see A's tag: %v", *gotB.Tag)
	}

	// Tag search is caller-scoped too.
	matchA, _, err := f.svc.Scan(ctx, f.userA, url.Values{"search": {"A label"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("A scan: %v", err)
	}
	found := false
	for _, w := range matchA {
		if w.Address == addr {
			found = true
		}
	}
	if !found {
		t.Error("A's tag search must match the wallet")
	}

	matchB, _, err := f.svc.Scan(ctx, userB, url.Values{"search": {"A label"}, "limit": {"200"}})
	if err != nil {
		t.Fatalf("B scan: %v", err)
	}
	for _, w := range matchB {
		if w.Address == addr {
			t.Error("B's search must never match A's private tag")
		}
	}
}

// TAG-I-02: clearing deletes the row; detail tag goes null.
func TestRepo_Tag_ClearDeletesRow(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()
	walletID, _ := tagFixtureWallet(t, f, 2)

	if _, err := f.svc.UpdateTag(ctx, f.userA, walletID, strPtr("temp")); err != nil {
		t.Fatalf("set tag: %v", err)
	}
	if _, err := f.svc.UpdateTag(ctx, f.userA, walletID, strPtr("   ")); err != nil {
		t.Fatalf("clear tag: %v", err)
	}

	var n int64
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_wallet_tags WHERE user_id = $1 AND wallet_id = $2`,
		f.userA, walletID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected tag row deleted, got %d", n)
	}

	detail, err := f.svc.Detail(ctx, f.userA, walletID, url.Values{})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Tag != nil {
		t.Errorf("expected null tag, got %v", *detail.Tag)
	}
}
