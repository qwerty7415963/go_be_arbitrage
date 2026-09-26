//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/walletgroup"
)

type walletGroupSuite struct {
	db       *pgxpool.Pool
	router   *gin.Engine
	tenantID uuid.UUID
	userA    uuid.UUID
	userB    uuid.UUID
	addrs    []string
}

func setupWalletGroupSuite(t *testing.T) *walletGroupSuite {
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", dbHost, dbPort))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping database (is docker-up running?): %v", err)
	}

	s := &walletGroupSuite{
		db:       pool,
		tenantID: uuid.New(),
		userA:    uuid.New(),
		userB:    uuid.New(),
	}

	run := time.Now().UnixNano()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		s.tenantID, fmt.Sprintf("wg-e2e-%d", run)); err != nil {
		pool.Close()
		t.Fatalf("create tenant: %v", err)
	}
	for i, uid := range []uuid.UUID{s.userA, s.userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'test-hash')`,
			uid, s.tenantID, fmt.Sprintf("wg-e2e-%d-%d@test.com", run, i)); err != nil {
			pool.Close()
			t.Fatalf("create user: %v", err)
		}
	}

	repo := walletgroup.NewRepository(pool)
	svc := walletgroup.NewService(repo)
	handler := walletgroup.NewHandler(svc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	handler.RegisterRoutes(router.Group("/api/v1"))
	s.router = router

	t.Cleanup(s.cleanup)
	return s
}

func (s *walletGroupSuite) cleanup() {
	ctx := context.Background()
	for _, addr := range s.addrs {
		s.db.Exec(ctx, `
			DELETE FROM group_wallet_members WHERE wallet_id =
			(SELECT id FROM tracked_wallets WHERE chain='evm' AND address=$1)`, addr)
		s.db.Exec(ctx, `DELETE FROM tracked_wallets WHERE chain='evm' AND address=$1`, addr)
	}
	s.db.Exec(ctx, `DELETE FROM user_wallet_groups WHERE user_id = ANY($1)`,
		[]uuid.UUID{s.userA, s.userB})
	s.db.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{s.userA, s.userB})
	s.db.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, s.tenantID)
	s.db.Close()
}

func (s *walletGroupSuite) track(addr string) string {
	s.addrs = append(s.addrs, addr)
	return addr
}

func (s *walletGroupSuite) request(t *testing.T, method, url, userID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func (s *walletGroupSuite) decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return resp
}

func (s *walletGroupSuite) errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	errObj, ok := s.decode(t, w)["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error body, got %s", w.Body.String())
	}
	code, _ := errObj["code"].(string)
	return code
}

func (s *walletGroupSuite) createGroup(t *testing.T, userID, name string) string {
	t.Helper()
	w := s.request(t, "POST", "/api/v1/groups", userID, map[string]string{"name": name})
	if w.Code != http.StatusCreated {
		t.Fatalf("create group %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	data := s.decode(t, w)["data"].(map[string]interface{})
	return data["id"].(string)
}

func (s *walletGroupSuite) uniqueAddr(t *testing.T) string {
	t.Helper()
	return s.track(fmt.Sprintf("0x%040x", time.Now().UnixNano()))
}

// E2E-06: list isolation — A sees only A's groups

func TestE2E_WalletGroup_ListIsolation(t *testing.T) {
	s := setupWalletGroupSuite(t)

	s.createGroup(t, s.userA.String(), "A Alpha")
	s.createGroup(t, s.userA.String(), "A Beta")

	w := s.request(t, "GET", "/api/v1/groups", s.userA.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("A list: expected 200, got %d", w.Code)
	}
	items := s.decode(t, w)["data"].([]interface{})
	if len(items) != 2 {
		t.Errorf("A should see 2 groups, got %d", len(items))
	}

	w = s.request(t, "GET", "/api/v1/groups", s.userB.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("B list: expected 200, got %d", w.Code)
	}
	if items := s.decode(t, w)["data"].([]interface{}); len(items) != 0 {
		t.Errorf("B should see 0 groups, got %d", len(items))
	}
}

// E2E-07: create + duplicate name → 201 then 409 GROUP-002

func TestE2E_WalletGroup_CreateDuplicate(t *testing.T) {
	s := setupWalletGroupSuite(t)

	w := s.request(t, "POST", "/api/v1/groups", s.userA.String(), map[string]string{"name": "Smart Money"})
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = s.request(t, "POST", "/api/v1/groups", s.userA.String(), map[string]string{"name": "Smart Money"})
	if w.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if code := s.errorCode(t, w); code != "GROUP-002" {
		t.Errorf("expected GROUP-002, got %s", code)
	}

	// Different user may reuse the name.
	w = s.request(t, "POST", "/api/v1/groups", s.userB.String(), map[string]string{"name": "Smart Money"})
	if w.Code != http.StatusCreated {
		t.Errorf("other user create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// E2E-08: idempotent add — adding wallet X twice creates one membership

func TestE2E_WalletGroup_IdempotentAdd(t *testing.T) {
	s := setupWalletGroupSuite(t)
	groupID := s.createGroup(t, s.userA.String(), "Idem")
	addr := s.uniqueAddr(t)

	for i := 1; i <= 2; i++ {
		w := s.request(t, "POST", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
			map[string]interface{}{"wallets": []string{addr}})
		if w.Code != http.StatusOK {
			t.Fatalf("add %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	var cnt int
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`,
		uuid.MustParse(groupID)).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("expected 1 membership after double add, got %d", cnt)
	}
}

// E2E-09: wallet X in two groups

func TestE2E_WalletGroup_MultiGroupMembership(t *testing.T) {
	s := setupWalletGroupSuite(t)
	smartMoney := s.createGroup(t, s.userA.String(), "Smart Money")
	whale := s.createGroup(t, s.userA.String(), "Whale")
	addr := s.uniqueAddr(t)

	for _, groupID := range []string{smartMoney, whale} {
		w := s.request(t, "POST", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
			map[string]interface{}{"wallets": []string{addr}})
		if w.Code != http.StatusOK {
			t.Fatalf("add to %s: expected 200, got %d: %s", groupID, w.Code, w.Body.String())
		}
	}

	for _, groupID := range []string{smartMoney, whale} {
		w := s.request(t, "GET", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: expected 200, got %d", groupID, w.Code)
		}
		items := s.decode(t, w)["data"].([]interface{})
		if len(items) != 1 {
			t.Errorf("group %s: expected 1 wallet, got %d", groupID, len(items))
		}
	}
}

// E2E-10: removing X from one group keeps it in the other

func TestE2E_WalletGroup_IsolatedRemove(t *testing.T) {
	s := setupWalletGroupSuite(t)
	smartMoney := s.createGroup(t, s.userA.String(), "Smart Money")
	whale := s.createGroup(t, s.userA.String(), "Whale")
	addr := s.uniqueAddr(t)

	for _, groupID := range []string{smartMoney, whale} {
		w := s.request(t, "POST", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
			map[string]interface{}{"wallets": []string{addr}})
		if w.Code != http.StatusOK {
			t.Fatalf("add: expected 200, got %d", w.Code)
		}
	}

	w := s.request(t, "DELETE", "/api/v1/groups/"+smartMoney+"/wallets", s.userA.String(),
		map[string]interface{}{"wallets": []string{addr}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = s.request(t, "GET", "/api/v1/groups/"+whale+"/wallets", s.userA.String(), nil)
	items := s.decode(t, w)["data"].([]interface{})
	if len(items) != 1 {
		t.Errorf("X must remain in Whale, got %d wallets", len(items))
	}
}

// E2E-11: delete group — memberships gone, wallets and other group intact

func TestE2E_WalletGroup_DeleteCleanup(t *testing.T) {
	s := setupWalletGroupSuite(t)
	smartMoney := s.createGroup(t, s.userA.String(), "Smart Money")
	whale := s.createGroup(t, s.userA.String(), "Whale")
	addr := s.uniqueAddr(t)

	for _, groupID := range []string{smartMoney, whale} {
		w := s.request(t, "POST", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
			map[string]interface{}{"wallets": []string{addr}})
		if w.Code != http.StatusOK {
			t.Fatalf("add: expected 200, got %d", w.Code)
		}
	}

	w := s.request(t, "DELETE", "/api/v1/groups/"+smartMoney, s.userA.String(), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var memberCnt, walletCnt int
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`,
		uuid.MustParse(smartMoney)).Scan(&memberCnt)
	if memberCnt != 0 {
		t.Errorf("expected memberships removed, got %d", memberCnt)
	}
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tracked_wallets WHERE chain='evm' AND address=$1`, addr).Scan(&walletCnt)
	if walletCnt != 1 {
		t.Errorf("expected wallet row intact, got %d", walletCnt)
	}

	w = s.request(t, "GET", "/api/v1/groups/"+whale+"/wallets", s.userA.String(), nil)
	items := s.decode(t, w)["data"].([]interface{})
	if len(items) != 1 {
		t.Errorf("Whale must keep the wallet, got %d", len(items))
	}
}

// E2E-12: cross-user attack — B patches/deletes A's group → blocked

func TestE2E_WalletGroup_CrossUserAttack(t *testing.T) {
	s := setupWalletGroupSuite(t)
	groupID := s.createGroup(t, s.userA.String(), "A Secure")

	w := s.request(t, "PATCH", "/api/v1/groups/"+groupID, s.userB.String(),
		map[string]string{"name": "Hijacked"})
	if w.Code != http.StatusForbidden {
		t.Errorf("B patch: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if code := s.errorCode(t, w); code != "GROUP-003" {
		t.Errorf("expected GROUP-003, got %s", code)
	}

	w = s.request(t, "DELETE", "/api/v1/groups/"+groupID, s.userB.String(), nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("B delete: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// A's group still exists with original name.
	w = s.request(t, "GET", "/api/v1/groups/"+groupID, s.userA.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("A get: expected 200, got %d", w.Code)
	}
	data := s.decode(t, w)["data"].(map[string]interface{})
	if data["name"] != "A Secure" {
		t.Errorf("expected original name, got %v", data["name"])
	}
}

// E2E-13: wallet identity — same address+chain added twice (different
// casing) reuses one tracked_wallets row (BR-01)

func TestE2E_WalletGroup_WalletIdentityNormalization(t *testing.T) {
	s := setupWalletGroupSuite(t)
	groupID := s.createGroup(t, s.userA.String(), "Identity")
	addr := s.uniqueAddr(t)
	upper := "0x" + addr[2:]

	for _, entry := range []string{addr, upper} {
		w := s.request(t, "POST", "/api/v1/groups/"+groupID+"/wallets", s.userA.String(),
			map[string]interface{}{"wallets": []string{entry}, "chain": "evm"})
		if w.Code != http.StatusOK {
			t.Fatalf("add %q: expected 200, got %d: %s", entry, w.Code, w.Body.String())
		}
	}

	var walletRows, memberRows int
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tracked_wallets WHERE chain='evm' AND address=$1`,
		addr).Scan(&walletRows)
	if walletRows != 1 {
		t.Errorf("expected single identity row, got %d", walletRows)
	}
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_wallet_members WHERE group_id = $1`,
		uuid.MustParse(groupID)).Scan(&memberRows)
	if memberRows != 1 {
		t.Errorf("expected single membership, got %d", memberRows)
	}
}
