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
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
	"github.com/qwerty7415963/go_be_arbitrage/internal/tradergroup"
)

type traderSuite struct {
	db      *pgxpool.Pool
	router  *gin.Engine
	userA   uuid.UUID
	userB   uuid.UUID
	venueID uuid.UUID
	addrs   []string
}

func traderSeedAddr(i int) string {
	return fmt.Sprintf("0x71ade%035x", i+1)
}

func setupTraderSuite(t *testing.T) *traderSuite {
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
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	s := &traderSuite{db: pool, userA: uuid.New(), userB: uuid.New()}
	run := time.Now().UnixNano()
	var tenantID uuid.UUID = uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		tenantID, fmt.Sprintf("trader-e2e-%d", run)); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	for i, uid := range []uuid.UUID{s.userA, s.userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'x')`,
			uid, tenantID, fmt.Sprintf("trader-e2e-%d-%d@test.com", run, i)); err != nil {
			t.Fatalf("user: %v", err)
		}
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'hyperliquid'`).Scan(&s.venueID); err != nil {
		t.Fatalf("venue: %v", err)
	}

	repo := trader.NewRepository(pool)
	pnls := []float64{5000, 15000, 25000}
	for i := 0; i < 3; i++ {
		addr := traderSeedAddr(i)
		s.addrs = append(s.addrs, addr)
		if _, _, err := repo.UpsertRegistry(ctx, s.venueID, addr,
			trader.SourceLeaderboard, nil, nil); err != nil {
			t.Fatalf("registry: %v", err)
		}
		pnl := pnls[i]
		tc := int64(4 + i)
		if err := repo.UpsertPeriodMetrics(ctx, &trader.PeriodMetrics{
			VenueID: s.venueID, WalletAddress: addr, Period: trader.Period30D,
			AsOf: time.Now().UTC(), PnL: &pnl, TradeCount: &tc,
			DataStatus: trader.DataReady, CalculationVersion: 1,
		}); err != nil {
			t.Fatalf("period: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, a := range s.addrs {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, a)
		}
	})

	secret := []byte("e2e-cursor-secret-32-bytes!!!!!")
	svc := trader.NewService(repo, tradergroup.NewRepository(pool), secret)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	trader.NewHandler(svc).RegisterRoutes(v1)
	tradergroup.NewHandler(tradergroup.NewRepository(pool)).RegisterRoutes(v1)
	s.router = router
	return s
}

func (s *traderSuite) doJSON(t *testing.T, method, target, userID string, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	resp := map[string]any{}
	if len(w.Body.Bytes()) > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, target, w.Body.String(), err)
		}
	}
	return w.Code, resp
}

func rowsOf(t *testing.T, resp map[string]any) []any {
	t.Helper()
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %v", resp)
	}
	return data
}

// E2E-T-01: search → cursor walk → detail → group lifecycle → group-filtered
// search → remove → delete (registry intact).
func TestE2E_Trader_FullFlow(t *testing.T) {
	s := setupTraderSuite(t)
	userA := s.userA.String()

	code, resp := s.doJSON(t, "POST", "/api/v1/traders/search", userA,
		`{"period":"30D","pnl_min":10000,"sort_by":"pnl","sort_direction":"desc","limit":1}`)
	if code != http.StatusOK {
		t.Fatalf("search: %d %v", code, resp)
	}
	rows := rowsOf(t, resp)
	if len(rows) != 1 || rows[0].(map[string]any)["pnl"] != 25000.0 {
		t.Fatalf("top row: %v", rows)
	}
	meta := resp["meta"].(map[string]any)
	if meta["has_more"] != true || meta["cursor"] == "" {
		t.Fatalf("cursor meta: %v", meta)
	}
	seen := map[string]bool{rows[0].(map[string]any)["wallet_address"].(string): true}
	cursor := meta["cursor"].(string)
	for i := 0; i < 5; i++ {
		code, resp = s.doJSON(t, "POST", "/api/v1/traders/search", userA,
			`{"period":"30D","pnl_min":10000,"limit":1,"cursor":"`+cursor+`"}`)
		if code != http.StatusOK {
			t.Fatalf("page: %d %v", code, resp)
		}
		rows = rowsOf(t, resp)
		for _, r := range rows {
			a := r.(map[string]any)["wallet_address"].(string)
			if seen[a] {
				t.Fatalf("duplicate %s", a)
			}
			seen[a] = true
		}
		meta = resp["meta"].(map[string]any)
		if meta["has_more"] != true {
			break
		}
		cursor = meta["cursor"].(string)
	}
	if len(seen) != 2 {
		t.Errorf("pnl>=10000 must yield 2 wallets, got %v", seen)
	}

	code, resp = s.doJSON(t, "GET", "/api/v1/traders/"+s.addrs[0]+"?period=30D", userA, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, resp)
	}
	if resp["data"].(map[string]any)["period"] != "30D" {
		t.Errorf("detail period: %v", resp)
	}

	code, resp = s.doJSON(t, "POST", "/api/v1/trader-groups", userA, `{"name":"Alphas"}`)
	if code != http.StatusCreated {
		t.Fatalf("create group: %d %v", code, resp)
	}
	gid := resp["data"].(map[string]any)["id"].(string)

	code, resp = s.doJSON(t, "POST", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"hyperliquid","wallet_address":"`+s.addrs[0]+`","alias":"w1"}]}`)
	if code != http.StatusOK {
		t.Fatalf("add: %d %v", code, resp)
	}
	code, resp = s.doJSON(t, "GET", "/api/v1/trader-groups/"+gid+"/members", userA, "")
	if code != http.StatusOK || len(rowsOf(t, resp)) != 1 {
		t.Fatalf("members: %d %v", code, resp)
	}
	code, resp = s.doJSON(t, "POST", "/api/v1/traders/search", userA,
		`{"period":"30D","group_id":"`+gid+`"}`)
	if code != http.StatusOK || len(rowsOf(t, resp)) != 1 {
		t.Fatalf("group search: %d %v", code, resp)
	}
	code, _ = s.doJSON(t, "DELETE", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"hyperliquid","wallet_address":"`+s.addrs[0]+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	code, _ = s.doJSON(t, "DELETE", "/api/v1/trader-groups/"+gid, userA, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/"+s.addrs[0], userA, "")
	if code != http.StatusOK {
		t.Fatalf("registry must survive group delete: %d", code)
	}
}

// E2E-T-02: isolation — B cannot touch A's groups; anonymous reads work,
// anonymous writes rejected.
func TestE2E_Trader_Isolation(t *testing.T) {
	s := setupTraderSuite(t)
	userA, userB := s.userA.String(), s.userB.String()

	code, resp := s.doJSON(t, "POST", "/api/v1/trader-groups", userA, `{"name":"Private"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, resp)
	}
	gid := resp["data"].(map[string]any)["id"].(string)

	for _, tc := range []struct {
		method, target string
		body           string
	}{
		{"GET", "/api/v1/trader-groups/" + gid, ""},
		{"PATCH", "/api/v1/trader-groups/" + gid, `{"name":"x"}`},
		{"DELETE", "/api/v1/trader-groups/" + gid, ""},
		{"GET", "/api/v1/trader-groups/" + gid + "/members", ""},
	} {
		if code, _ := s.doJSON(t, tc.method, tc.target, userB, tc.body); code != http.StatusForbidden {
			t.Errorf("%s %s as B: want 403, got %d", tc.method, tc.target, code)
		}
	}
	if code, _ := s.doJSON(t, "POST", "/api/v1/traders/search", userB,
		`{"period":"30D","group_id":"`+gid+`"}`); code != http.StatusForbidden {
		t.Errorf("B group search: want 403, got %d", code)
	}
	if code, _ := s.doJSON(t, "POST", "/api/v1/traders/search", "",
		`{"period":"30D","limit":2}`); code != http.StatusOK {
		t.Errorf("anonymous search: want 200, got %d", code)
	}
	if code, _ := s.doJSON(t, "POST", "/api/v1/trader-groups", "",
		`{"name":"Anon"}`); code != http.StatusForbidden {
		t.Errorf("anonymous create: want 403, got %d", code)
	}
}
