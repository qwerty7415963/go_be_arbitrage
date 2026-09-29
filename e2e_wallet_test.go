//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
	"github.com/qwerty7415963/go_be_arbitrage/internal/walletgroup"
)

type walletSuite struct {
	db       *pgxpool.Pool
	router   *gin.Engine
	tenantID uuid.UUID
	userA    uuid.UUID
	userB    uuid.UUID
	addrs    []string
	walletID map[string]uuid.UUID
}

const walletSeedCount = 6

func walletSeedAddr(i int) string {
	return fmt.Sprintf("0xe2e%037x", i+1)
}

func setupWalletSuite(t *testing.T) *walletSuite {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", dbHost, dbPort))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping database (is docker-up running?): %v", err)
	}

	s := &walletSuite{
		db:       pool,
		tenantID: uuid.New(),
		userA:    uuid.New(),
		userB:    uuid.New(),
		walletID: map[string]uuid.UUID{},
	}

	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		s.tenantID, fmt.Sprintf("wallet-e2e-%d", run)); err != nil {
		pool.Close()
		t.Fatalf("create tenant: %v", err)
	}
	for i, uid := range []uuid.UUID{s.userA, s.userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'test-hash')`,
			uid, s.tenantID, fmt.Sprintf("wallet-e2e-%d-%d@test.com", run, i)); err != nil {
			pool.Close()
			t.Fatalf("create user: %v", err)
		}
	}

	// Seed venue + wallets + 30D snapshots (deterministic metrics).
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('e2e-dex', 'E2E DEX', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		pool.Close()
		t.Fatalf("venue: %v", err)
	}
	var venueID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code='e2e-dex'`).Scan(&venueID); err != nil {
		pool.Close()
		t.Fatalf("load venue: %v", err)
	}

	now := time.Now().UTC()
	for i := 0; i < walletSeedCount; i++ {
		addr := walletSeedAddr(i)
		var id uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO tracked_wallets (chain, address, venue_id)
			VALUES ('evm', $1, $2)
			ON CONFLICT (chain, address) DO UPDATE SET venue_id = EXCLUDED.venue_id
			RETURNING id`, addr, venueID).Scan(&id)
		if err != nil {
			pool.Close()
			t.Fatalf("seed wallet: %v", err)
		}
		s.addrs = append(s.addrs, addr)
		s.walletID[addr] = id

		pnl := float64((i + 1) * 1000)
		volume := float64((i + 1) * 10000)
		winRate := float64(40 + i*5)
		trades := int64(i + 1)
		if _, err := pool.Exec(ctx, `
			INSERT INTO wallet_metric_snapshots
			    (wallet_id, timeframe, realized_pnl, volume, win_rate, trade_count,
			     long_count, short_count, last_active_at, computed_at)
			VALUES ($1, '30D', $2, $3, $4, $5, $5, 1, $6, NOW())
			ON CONFLICT DO NOTHING`,
			id, pnl, volume, winRate, trades, now.Add(-time.Duration(i)*time.Hour)); err != nil {
			pool.Close()
			t.Fatalf("seed snapshot: %v", err)
		}
	}

	// Router: group endpoints (Phase 1) + scanner endpoints (Phase 2) with
	// the test auth shim.
	walletRepo := wallet.NewRepository(pool)
	cfg, err := walletRepo.LoadFilterConfig(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("filter config: %v", err)
	}
	walletHandler := wallet.NewHandler(wallet.NewService(walletRepo, *cfg))

	groupRepo := walletgroup.NewRepository(pool)
	groupHandler := walletgroup.NewHandler(walletgroup.NewService(groupRepo))
	groupHandler.SetScanner(wallet.NewService(walletRepo, *cfg))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	groupHandler.RegisterRoutes(router.Group("/api/v1"))
	walletHandler.RegisterRoutes(router.Group("/api/v1"))
	s.router = router

	t.Cleanup(s.cleanup)
	return s
}

func (s *walletSuite) cleanup() {
	ctx := context.Background()
	s.db.Exec(ctx, `
		DELETE FROM wallet_metric_snapshots WHERE wallet_id IN (
			SELECT id FROM tracked_wallets WHERE address = ANY($1))`, s.addrs)
	s.db.Exec(ctx, `
		DELETE FROM group_wallet_members WHERE wallet_id IN (
			SELECT id FROM tracked_wallets WHERE address = ANY($1))`, s.addrs)
	s.db.Exec(ctx, `DELETE FROM user_wallet_groups WHERE user_id = ANY($1)`,
		[]uuid.UUID{s.userA, s.userB})
	s.db.Exec(ctx, `DELETE FROM tracked_wallets WHERE address = ANY($1)`, s.addrs)
	s.db.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`,
		[]uuid.UUID{s.userA, s.userB})
	s.db.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, s.tenantID)
	s.db.Close()
}

func (s *walletSuite) get(t *testing.T, target, userID string) (int, map[string]interface{}) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Test-User", userID)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var resp map[string]interface{}
	if len(w.Body.Bytes()) > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, resp
}

func (s *walletSuite) postJSON(t *testing.T, target, userID string, body interface{}) (int, map[string]interface{}) {
	return s.doJSON(t, http.MethodPost, target, userID, body)
}

func (s *walletSuite) patchJSON(t *testing.T, target, userID string, body interface{}) (int, map[string]interface{}) {
	return s.doJSON(t, http.MethodPatch, target, userID, body)
}

func (s *walletSuite) doJSON(t *testing.T, method, target, userID string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(method, target, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", userID)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	var resp map[string]interface{}
	if len(w.Body.Bytes()) > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, resp
}

// E2E-14: scanner flow over HTTP — filter + sort + paginate.
func TestE2E_Wallet_ScanFilterSortPaginate(t *testing.T) {
	s := setupWalletSuite(t)

	code, resp := s.get(t, "/api/v1/wallets?pnl_gt=2500&sort=pnl&order=asc&limit=2", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, resp)
	}
	data, _ := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 rows (limit 2), got %d", len(data))
	}
	prevPnl := -1.0
	for _, row := range data {
		m := row.(map[string]interface{})
		metrics := m["metrics"].(map[string]interface{})
		pnl := metrics["realized_pnl"].(float64)
		if pnl <= 2500 {
			t.Errorf("pnl %v violates filter", pnl)
		}
		if pnl < prevPnl {
			t.Errorf("ascending order violated: %v after %v", pnl, prevPnl)
		}
		prevPnl = pnl
	}

	// Page 2 continues without duplicates (pnl 3000,4000 taken on page 1).
	code, resp = s.get(t, "/api/v1/wallets?pnl_gt=2500&sort=pnl&order=asc&limit=2&page=2", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d", code)
	}
	page2, _ := resp["data"].([]interface{})
	if len(page2) == 0 {
		t.Fatal("page 2 must not be empty")
	}
	page1Addrs := map[string]bool{}
	for _, row := range data {
		page1Addrs[row.(map[string]interface{})["address"].(string)] = true
	}
	for _, row := range page2 {
		addr := row.(map[string]interface{})["address"].(string)
		if page1Addrs[addr] {
			t.Errorf("duplicate wallet across pages: %s", addr)
		}
	}

	// Invalid operator → 400 COMMON-902 over HTTP.
	code, resp = s.get(t, "/api/v1/wallets?pnl_approx=1", s.userA.String())
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", code, resp)
	}
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["code"] != "COMMON-902" {
		t.Errorf("expected COMMON-902, got %v", errObj["code"])
	}
}

// E2E-15: group-scoped scanner (BE-09) — A filters own group; B blocked.
func TestE2E_Wallet_GroupScannerOwnership(t *testing.T) {
	s := setupWalletSuite(t)

	// A creates a group with the first 3 wallets.
	code, resp := s.postJSON(t, "/api/v1/groups", s.userA.String(),
		map[string]string{"name": "E2E Scanner Group"})
	if code != http.StatusCreated {
		t.Fatalf("create group: %d %v", code, resp)
	}
	groupID := resp["data"].(map[string]interface{})["id"].(string)

	wallets := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		wallets = append(wallets, s.walletID[walletSeedAddr(i)].String())
	}
	code, resp = s.postJSON(t, "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
		map[string]interface{}{"wallets": wallets})
	if code != http.StatusOK {
		t.Fatalf("add wallets: %d %v", code, resp)
	}

	// A filters: only group wallets with pnl in (1000, 3000) → i=0 (1000)?
	// pnl values: 1000..6000; group holds 1000,2000,3000 → gt 1500 → 2000,3000.
	code, resp = s.get(t, "/api/v1/groups/"+groupID+"/wallets?pnl_gt=1500&sort=pnl&order=asc", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("group scan: %d %v", code, resp)
	}
	data, _ := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 filtered group rows, got %d", len(data))
	}
	for _, row := range data {
		addr := row.(map[string]interface{})["address"].(string)
		if addr != walletSeedAddr(1) && addr != walletSeedAddr(2) {
			t.Errorf("wallet %s is not in the filtered group set", addr)
		}
		metrics := row.(map[string]interface{})["metrics"].(map[string]interface{})
		if metrics["realized_pnl"] == nil {
			t.Errorf("group scanner rows must carry metrics")
		}
	}

	// B tries the same group → 403 GROUP-003 (exists but foreign).
	code, resp = s.get(t, "/api/v1/groups/"+groupID+"/wallets?pnl_gt=1500", s.userB.String())
	if code != http.StatusForbidden {
		t.Fatalf("B: expected 403, got %d: %v", code, resp)
	}
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["code"] != "GROUP-003" {
		t.Errorf("expected GROUP-003, got %v", errObj["code"])
	}
}

// E2E-16: wallet detail isolation (BE-06) — the wallet lives only in B's
// group; A gets the wallet with zero B memberships.
func TestE2E_Wallet_Detail_NoForeignMembershipLeak(t *testing.T) {
	s := setupWalletSuite(t)

	// B creates a group containing wallet 5.
	code, resp := s.postJSON(t, "/api/v1/groups", s.userB.String(),
		map[string]string{"name": "B Secret Group"})
	if code != http.StatusCreated {
		t.Fatalf("B create group: %d %v", code, resp)
	}
	groupB := resp["data"].(map[string]interface{})["id"].(string)

	targetAddr := walletSeedAddr(4)
	code, resp = s.postJSON(t, "/api/v1/groups/"+groupB+"/wallets", s.userB.String(),
		map[string]interface{}{"wallets": []string{s.walletID[targetAddr].String()}})
	if code != http.StatusOK {
		t.Fatalf("B add wallet: %d %v", code, resp)
	}

	// A queries the detail.
	code, resp = s.get(t, "/api/v1/wallets/"+s.walletID[targetAddr].String(), s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, resp)
	}
	data := resp["data"].(map[string]interface{})
	if data["address"] != targetAddr {
		t.Errorf("identity mismatch: %v", data["address"])
	}
	if data["metrics"] == nil {
		t.Error("expected metrics")
	}
	memberships, _ := data["memberships"].([]interface{})
	if len(memberships) != 0 {
		t.Errorf("B's membership leaked to A: %v", memberships)
	}
	if strings.Contains(t.Name(), "leak") && strings.Contains(fmt.Sprint(resp), "B Secret Group") {
		t.Error("foreign group name in response")
	}

	// B still sees the membership.
	code, resp = s.get(t, "/api/v1/wallets/"+s.walletID[targetAddr].String(), s.userB.String())
	if code != http.StatusOK {
		t.Fatalf("B detail: %d", code)
	}
	memberships, _ = resp["data"].(map[string]interface{})["memberships"].([]interface{})
	if len(memberships) != 1 {
		t.Errorf("B should see own membership, got %v", memberships)
	}
}

// TAG-E2E: tag round-trip over HTTP — PATCH → detail → scan search, with
// per-user isolation.
func TestE2E_Wallet_TagRoundTrip(t *testing.T) {
	s := setupWalletSuite(t)
	addr := walletSeedAddr(0)
	id := s.walletID[addr].String()

	// A sets a tag.
	code, resp := s.patchJSON(t, "/api/v1/wallets/"+id, s.userA.String(),
		map[string]string{"tag": "E2E Tag"})
	if code != http.StatusOK {
		t.Fatalf("PATCH: %d %v", code, resp)
	}
	if resp["data"].(map[string]interface{})["tag"] != "E2E Tag" {
		t.Errorf("tag not in PATCH response: %v", resp["data"])
	}

	// A reads it back on detail.
	code, resp = s.get(t, "/api/v1/wallets/"+id, s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("detail: %d", code)
	}
	if resp["data"].(map[string]interface{})["tag"] != "E2E Tag" {
		t.Errorf("tag not in detail: %v", resp["data"])
	}

	// A finds it via tag search.
	code, resp = s.get(t, "/api/v1/wallets?search=E2E%20Tag", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("scan: %d", code)
	}
	rows, _ := resp["data"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row via tag search, got %d", len(rows))
	}
	if rows[0].(map[string]interface{})["address"] != addr {
		t.Errorf("wrong wallet via tag search: %v", rows[0])
	}

	// B sees no tag anywhere.
	code, resp = s.get(t, "/api/v1/wallets/"+id, s.userB.String())
	if code != http.StatusOK {
		t.Fatalf("B detail: %d", code)
	}
	if tag, _ := resp["data"].(map[string]interface{})["tag"]; tag != nil {
		t.Errorf("B must not see A's tag: %v", tag)
	}
	code, resp = s.get(t, "/api/v1/wallets?search=E2E%20Tag", s.userB.String())
	if code != http.StatusOK {
		t.Fatalf("B scan: %d", code)
	}
	if rows, _ := resp["data"].([]interface{}); len(rows) != 0 {
		t.Errorf("B's search must not match A's tag: %v", rows)
	}

	// A clears the tag.
	code, resp = s.patchJSON(t, "/api/v1/wallets/"+id, s.userA.String(),
		map[string]string{"tag": ""})
	if code != http.StatusOK {
		t.Fatalf("clear: %d %v", code, resp)
	}
	if tag, _ := resp["data"].(map[string]interface{})["tag"]; tag != nil {
		t.Errorf("expected null tag after clear: %v", tag)
	}

	// Unknown wallet → 404 WALLET-001.
	code, resp = s.patchJSON(t, "/api/v1/wallets/00000000-0000-0000-0000-000000000000",
		s.userA.String(), map[string]string{"tag": "x"})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %v", code, resp)
	}
	if resp["error"].(map[string]interface{})["code"] != "WALLET-001" {
		t.Errorf("expected WALLET-001: %v", resp["error"])
	}
}

// WL-E2E: watchlist round-trip over HTTP — star → scan filter → detail →
// unstar, with per-user isolation (B never sees A's stars).
func TestE2E_Wallet_WatchlistRoundTrip(t *testing.T) {
	s := setupWalletSuite(t)
	addr := walletSeedAddr(1)
	id := s.walletID[addr].String()

	// A stars the wallet.
	code, resp := s.patchJSON(t, "/api/v1/wallets/"+id, s.userA.String(),
		map[string]bool{"watchlisted": true})
	if code != http.StatusOK {
		t.Fatalf("star: %d %v", code, resp)
	}
	if resp["data"].(map[string]interface{})["watchlisted"] != true {
		t.Errorf("star not in PATCH response: %v", resp["data"])
	}

	// A's watchlist filter returns it, unfiltered scan marks it.
	code, resp = s.get(t, "/api/v1/wallets?watchlisted=true", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("A scan: %d %v", code, resp)
	}
	rows, _ := resp["data"].([]interface{})
	if len(rows) != 1 {
		t.Fatalf("A: expected 1 starred wallet, got %d", len(rows))
	}
	if rows[0].(map[string]interface{})["address"] != addr {
		t.Errorf("wrong starred wallet: %v", rows[0])
	}

	// B's watchlist filter is empty (per-user stars).
	code, resp = s.get(t, "/api/v1/wallets?watchlisted=true", s.userB.String())
	if code != http.StatusOK {
		t.Fatalf("B scan: %d %v", code, resp)
	}
	if rows, _ := resp["data"].([]interface{}); len(rows) != 0 {
		t.Errorf("B must not see A's stars: %v", rows)
	}
	code, resp = s.get(t, "/api/v1/wallets/"+id, s.userB.String())
	if wl := resp["data"].(map[string]interface{})["watchlisted"]; wl != false {
		t.Errorf("B detail watchlisted: %v", wl)
	}

	// A unstars → filter empty again.
	code, resp = s.patchJSON(t, "/api/v1/wallets/"+id, s.userA.String(),
		map[string]bool{"watchlisted": false})
	if code != http.StatusOK {
		t.Fatalf("unstar: %d %v", code, resp)
	}
	code, resp = s.get(t, "/api/v1/wallets?watchlisted=true", s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("A scan after unstar: %d %v", code, resp)
	}
	if rows, _ := resp["data"].([]interface{}); len(rows) != 0 {
		t.Errorf("expected empty watchlist after unstar: %v", rows)
	}

	// Empty body → 400 COMMON-902.
	code, resp = s.patchJSON(t, "/api/v1/wallets/"+id, s.userA.String(), map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("empty PATCH: expected 400, got %d: %v", code, resp)
	}
}

// POS-E2E: per-market snapshots flow into the detail drawer's positions
// breakdown (sorted by pnl, timeframe-scoped).
func TestE2E_Wallet_DetailPositionsBreakdown(t *testing.T) {
	s := setupWalletSuite(t)
	addr := walletSeedAddr(2)
	id := s.walletID[addr].String()

	// Seed per-market 30D snapshots for this wallet.
	for _, row := range []struct {
		market string
		pnl    float64
	}{{"BTC", 21000}, {"ETH", 14000}} {
		if _, err := s.db.Exec(context.Background(), `
			INSERT INTO wallet_metric_snapshots
			    (wallet_id, market, timeframe, realized_pnl, volume, trade_count, computed_at)
			VALUES ($1, $2, '30D', $3, $3, 1, NOW())
			ON CONFLICT DO NOTHING`, s.walletID[addr], row.market, row.pnl); err != nil {
			t.Fatalf("seed %s snapshot: %v", row.market, err)
		}
	}

	code, resp := s.get(t, "/api/v1/wallets/"+id, s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, resp)
	}
	data := resp["data"].(map[string]interface{})
	positions, ok := data["positions"].([]interface{})
	if !ok {
		t.Fatalf("positions missing: %v", data)
	}
	if len(positions) != 2 {
		t.Fatalf("expected 2 positions, got %d: %v", len(positions), positions)
	}
	first := positions[0].(map[string]interface{})
	if first["market"] != "BTC" || first["realized_pnl"].(float64) != 21000 {
		t.Errorf("expected BTC first (higher pnl): %v", first)
	}

	// A wallet without per-market rows → [] (never null).
	other := s.walletID[walletSeedAddr(3)].String()
	code, resp = s.get(t, "/api/v1/wallets/"+other, s.userA.String())
	if code != http.StatusOK {
		t.Fatalf("detail 2: %d", code)
	}
	if positions, _ := resp["data"].(map[string]interface{})["positions"].([]interface{}); positions == nil {
		t.Error("positions must be an empty array, not null")
	}
}
