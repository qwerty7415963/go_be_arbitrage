//go:build integration

package walletgroup

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

type fixture struct {
	pool     *pgxpool.Pool
	repo     *Repository
	svc      *Service
	tenantID uuid.UUID
	userA    uuid.UUID
	userB    uuid.UUID
	addrs    []string // (chain, address) identities to remove on cleanup
}

func setupWalletGroupFixture(t *testing.T) *fixture {
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
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping (is docker-up running?): %v", err)
	}

	f := &fixture{
		pool:     pool,
		repo:     NewRepository(pool),
		tenantID: uuid.New(),
		userA:    uuid.New(),
		userB:    uuid.New(),
	}
	f.svc = NewService(f.repo)

	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		f.tenantID, fmt.Sprintf("wg-fixture-%d", run)); err != nil {
		pool.Close()
		t.Fatalf("create tenant: %v", err)
	}
	for i, uid := range []uuid.UUID{f.userA, f.userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'test-hash')`,
			uid, f.tenantID, fmt.Sprintf("wg-fixture-%d-%d@test.com", run, i)); err != nil {
			pool.Close()
			t.Fatalf("create user: %v", err)
		}
	}

	t.Cleanup(func() {
		ctx := context.Background()
		if len(f.addrs) > 0 {
			pool.Exec(ctx, `
				DELETE FROM group_wallet_members m
				USING tracked_wallets w
				WHERE m.wallet_id = w.id
				  AND (w.chain, w.address) IN (
				      SELECT * FROM unnest($1::text[], $2::text[])
				  )`,
				f.chainSlice(), f.addrSlice())
		}
		pool.Exec(ctx, `DELETE FROM user_wallet_groups WHERE user_id = ANY($1)`, []uuid.UUID{f.userA, f.userB})
		if len(f.addrs) > 0 {
			pool.Exec(ctx, `DELETE FROM tracked_wallets WHERE (chain, address) IN (
				SELECT * FROM unnest($1::text[], $2::text[])
			)`, f.chainSlice(), f.addrSlice())
		}
		pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{f.userA, f.userB})
		pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
		pool.Close()
	})

	return f
}

func (f *fixture) chainSlice() []string {
	out := make([]string, 0, len(f.addrs))
	for range f.addrs {
		out = append(out, "evm")
	}
	return out
}

func (f *fixture) addrSlice() []string {
	out := make([]string, 0, len(f.addrs))
	for _, a := range f.addrs {
		out = append(out, a)
	}
	return out
}

// trackAddr registers a canonical address (lowercase, 0x…) for cleanup.
func (f *fixture) trackAddr(addr string) string {
	f.addrs = append(f.addrs, addr)
	return addr
}

func (f *fixture) createGroup(t *testing.T, userID uuid.UUID, name string) *Group {
	t.Helper()
	g, err := f.svc.CreateGroup(context.Background(), userID, &CreateGroupRequest{Name: name})
	if err != nil {
		t.Fatalf("create group %q: %v", name, err)
	}
	return g
}

// ─── GRP-I-01: CreateGroup success ──────────────────────────────

func TestRepo_CreateGroup_Success(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-01 Group")

	loaded, err := f.repo.GetGroupByID(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if loaded.Name != "I-01 Group" || loaded.UserID != f.userA {
		t.Errorf("unexpected group %+v", loaded)
	}
	if loaded.WalletCount != 0 {
		t.Errorf("expected wallet_count 0, got %d", loaded.WalletCount)
	}
}

// ─── GRP-I-02: duplicate (user_id, name) → GROUP-002 ────────────

func TestRepo_CreateGroup_DuplicateName(t *testing.T) {
	f := setupWalletGroupFixture(t)
	f.createGroup(t, f.userA, "Dup Name")

	// Repository surfaces the UNIQUE violation as ErrDuplicateName…
	err := f.repo.CreateGroup(context.Background(), &Group{
		ID: uuid.New(), UserID: f.userA, Name: "Dup Name",
	})
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}

	// …and the service maps it to GROUP-002 (409).
	_, svcErr := f.svc.CreateGroup(context.Background(), f.userA, &CreateGroupRequest{Name: "Dup Name"})
	var appErr *domain.AppError
	if !errors.As(svcErr, &appErr) || appErr.Code != domain.ErrCodeGroupDuplicate {
		t.Fatalf("expected GROUP-002, got %v", svcErr)
	}
}

// ─── GRP-I-03: ListGroups returns only caller's groups ──────────

func TestRepo_ListGroups_OnlyOwnGroups(t *testing.T) {
	f := setupWalletGroupFixture(t)
	f.createGroup(t, f.userA, "A-1")
	f.createGroup(t, f.userA, "A-2")
	f.createGroup(t, f.userB, "B-1")

	groupsA, err := f.repo.ListGroups(context.Background(), f.userA)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(groupsA) != 2 {
		t.Fatalf("expected 2 groups for A, got %d", len(groupsA))
	}
	for _, g := range groupsA {
		if g.UserID != f.userA {
			t.Errorf("leaked group of another user: %+v", g)
		}
	}

	groupsB, err := f.repo.ListGroups(context.Background(), f.userB)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(groupsB) != 1 || groupsB[0].Name != "B-1" {
		t.Errorf("expected exactly B-1 for B, got %+v", groupsB)
	}
}

// ─── GRP-I-04: DeleteGroup cleans group + members, wallets stay ─

func TestRepo_DeleteGroup_CleansMembersKeepsWallets(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-04")
	addr := f.trackAddr("0x" + fmt.Sprintf("%040x", time.Now().UnixNano()))

	if _, err := f.svc.AddWallets(context.Background(), f.userA, g.ID, &WalletsRequest{
		Wallets: []string{addr}, Chain: "evm",
	}); err != nil {
		t.Fatalf("add wallet: %v", err)
	}

	rows, err := f.repo.DeleteGroup(context.Background(), g.ID, f.userA)
	if err != nil || rows != 1 {
		t.Fatalf("delete group: rows=%d err=%v", rows, err)
	}

	// Group and memberships are gone…
	var cnt int
	f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`, g.ID).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("expected memberships removed, got %d", cnt)
	}
	if _, err := f.repo.GetGroupByID(context.Background(), g.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected group gone, got %v", err)
	}

	// …tracked_wallets (BR-10) remains.
	var exists bool
	f.pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM tracked_wallets WHERE chain='evm' AND address=$1)`,
		addr).Scan(&exists)
	if !exists {
		t.Error("expected tracked_wallets row to remain after group delete")
	}
}

// ─── GRP-I-05: AddMembers idempotent (ON CONFLICT DO NOTHING) ───

func TestRepo_AddMembers_IdempotentInsert(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-05")
	addr := f.trackAddr("0x" + fmt.Sprintf("%040x", time.Now().UnixNano()))

	items := []walletItem{{chain: "evm", address: addr}}
	created, err := f.repo.AddMembersTx(context.Background(), g.ID, f.userA, items)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	if created != 1 {
		t.Errorf("expected 1 created, got %d", created)
	}

	created, err = f.repo.AddMembersTx(context.Background(), g.ID, f.userA, items)
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	if created != 0 {
		t.Errorf("expected 0 created on re-add, got %d", created)
	}

	var cnt int
	f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`, g.ID).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("expected exactly 1 row, got %d", cnt)
	}
}

// ─── GRP-I-06: PK (group_id, wallet_id) rejects duplicates ──────

func TestRepo_Membership_PKRejectsDuplicate(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-06")
	addr := f.trackAddr("0x" + fmt.Sprintf("%040x", time.Now().UnixNano()))

	items := []walletItem{{chain: "evm", address: addr}}
	if _, err := f.repo.AddMembersTx(context.Background(), g.ID, f.userA, items); err != nil {
		t.Fatalf("add: %v", err)
	}

	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO group_wallet_members (group_id, wallet_id, added_by)
		SELECT $1, id, $3 FROM tracked_wallets WHERE chain='evm' AND address=$2`,
		g.ID, addr, f.userA)
	if err == nil {
		t.Fatal("expected PK violation on duplicate membership")
	}
}

// ─── GRP-I-07: wallet in 2 groups (BR-02) ───────────────────────

func TestRepo_WalletInTwoGroups(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g1 := f.createGroup(t, f.userA, "I-07-One")
	g2 := f.createGroup(t, f.userA, "I-07-Two")
	addr := f.trackAddr("0x" + fmt.Sprintf("%040x", time.Now().UnixNano()))

	for _, g := range []*Group{g1, g2} {
		if _, err := f.svc.AddWallets(context.Background(), f.userA, g.ID, &WalletsRequest{
			Wallets: []string{addr},
		}); err != nil {
			t.Fatalf("add to %s: %v", g.Name, err)
		}
	}

	if _, err := f.svc.RemoveWallets(context.Background(), f.userA, g1.ID, &WalletsRequest{
		Wallets: []string{addr},
	}); err != nil {
		t.Fatalf("remove from g1: %v", err)
	}

	var cnt int
	f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE wallet_id =
		 (SELECT id FROM tracked_wallets WHERE chain='evm' AND address=$1)`,
		addr).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("expected membership to remain in second group, total %d", cnt)
	}
	var g2cnt int
	f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`, g2.ID).Scan(&g2cnt)
	if g2cnt != 1 {
		t.Errorf("expected wallet still in group 2, got %d", g2cnt)
	}
}

// ─── GRP-I-08: RemoveMembers bulk ───────────────────────────────

func TestRepo_RemoveMembers_Bulk(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-08")

	var items []walletItem
	var entries []string
	for i := 0; i < 3; i++ {
		addr := f.trackAddr(fmt.Sprintf("0x%02d%038d", i, i))
		items = append(items, walletItem{chain: "evm", address: addr})
		entries = append(entries, addr)
	}
	if _, err := f.repo.AddMembersTx(context.Background(), g.ID, f.userA, items); err != nil {
		t.Fatalf("seed memberships: %v", err)
	}

	removed, err := f.repo.RemoveMembers(context.Background(), g.ID, items[:2])
	if err != nil {
		t.Fatalf("bulk remove: %v", err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	wallets, total, err := f.repo.ListMembers(context.Background(), g.ID, "", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(wallets) != 1 || wallets[0].Address != entries[2] {
		t.Errorf("expected only third wallet to remain, got total=%d wallets=%+v", total, wallets)
	}
}

// ─── GRP-I-09: ownership on foreign/unknown group ───────────────

func TestRepo_GetGroup_OwnershipEnforcement(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-09")

	// Owner can load it.
	if _, err := f.svc.GetGroup(context.Background(), f.userA, g.ID); err != nil {
		t.Fatalf("owner should load group: %v", err)
	}

	// Foreign user → GROUP-003.
	_, err := f.svc.GetGroup(context.Background(), f.userB, g.ID)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeGroupForbidden {
		t.Fatalf("expected GROUP-003, got %v", err)
	}

	// Unknown id → GROUP-001.
	_, err = f.svc.GetGroup(context.Background(), f.userA, uuid.New())
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeGroupNotFound {
		t.Fatalf("expected GROUP-001, got %v", err)
	}
}

// ─── GRP-I-10: bulk add rollback, no partial membership ─────────

func TestRepo_AddMembersTx_PartialFailureRollsBack(t *testing.T) {
	f := setupWalletGroupFixture(t)
	g := f.createGroup(t, f.userA, "I-10")

	addr := f.trackAddr("0x" + fmt.Sprintf("%040x", time.Now().UnixNano()))
	var walletID uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO tracked_wallets (chain, address) VALUES ('evm', $1) RETURNING id`,
		addr).Scan(&walletID); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}

	// First item valid, second unknown → whole batch must roll back.
	_, err := f.repo.AddMembersTx(context.Background(), g.ID, f.userA, []walletItem{
		{isID: true, id: walletID},
		{isID: true, id: uuid.New()},
	})
	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}

	var cnt int
	f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`, g.ID).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("expected rollback to leave 0 memberships, got %d", cnt)
	}
}
