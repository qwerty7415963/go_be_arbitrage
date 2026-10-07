//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
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

// traderRandAddr returns a crypto-rand 0x + 40 hex address (collision-safe
// across parallel runs; never time-derived).
func traderRandAddr(t *testing.T) string {
	t.Helper()
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "0x" + hex.EncodeToString(b[:])
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
	if _, err := pool.Exec(ctx, `
		INSERT INTO venues (code, name, venue_type)
		VALUES ('trader-e2e-venue', 'Trader E2E Venue', 'PERP_DEX')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("venue upsert: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = 'trader-e2e-venue'`).Scan(&s.venueID); err != nil {
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
		`{"period":"30D","venue":"trader-e2e-venue","pnl_min":10000,"sort_by":"pnl","sort_direction":"desc","limit":1}`)
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
			`{"period":"30D","venue":"trader-e2e-venue","pnl_min":10000,"limit":1,"cursor":"`+cursor+`"}`)
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

	code, resp = s.doJSON(t, "GET", "/api/v1/traders/"+s.addrs[0]+"?venue=trader-e2e-venue&period=30D", userA, "")
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
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[0]+`","alias":"w1"}]}`)
	if code != http.StatusOK {
		t.Fatalf("add: %d %v", code, resp)
	}
	code, resp = s.doJSON(t, "GET", "/api/v1/trader-groups/"+gid+"/members", userA, "")
	if code != http.StatusOK || len(rowsOf(t, resp)) != 1 {
		t.Fatalf("members: %d %v", code, resp)
	}
	code, resp = s.doJSON(t, "POST", "/api/v1/traders/search", userA,
		`{"period":"30D","venue":"trader-e2e-venue","group_id":"`+gid+`"}`)
	if code != http.StatusOK || len(rowsOf(t, resp)) != 1 {
		t.Fatalf("group search: %d %v", code, resp)
	}
	code, _ = s.doJSON(t, "DELETE", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[0]+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	code, _ = s.doJSON(t, "DELETE", "/api/v1/trader-groups/"+gid, userA, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/"+s.addrs[0]+"?venue=trader-e2e-venue", userA, "")
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
		`{"period":"30D","venue":"trader-e2e-venue","group_id":"`+gid+`"}`); code != http.StatusForbidden {
		t.Errorf("B group search: want 403, got %d", code)
	}
	if code, _ := s.doJSON(t, "POST", "/api/v1/traders/search", "",
		`{"period":"30D","venue":"trader-e2e-venue","limit":2}`); code != http.StatusOK {
		t.Errorf("anonymous search: want 200, got %d", code)
	}
	if code, _ := s.doJSON(t, "POST", "/api/v1/trader-groups", "",
		`{"name":"Anon"}`); code != http.StatusForbidden {
		t.Errorf("anonymous create: want 403, got %d", code)
	}
}

// E2E-T-04 (BE-1/BE-2): members carry period metrics; PATCH alias/note
// round-trips (set → clear-to-null), period param scopes metrics.
func TestE2E_Trader_MemberPatchAndMetrics(t *testing.T) {
	s := setupTraderSuite(t)
	userA := s.userA.String()

	code, resp := s.doJSON(t, "POST", "/api/v1/trader-groups", userA, `{"name":"M"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, resp)
	}
	gid := resp["data"].(map[string]any)["id"].(string)

	code, _ = s.doJSON(t, "POST", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[0]+`","alias":"w1","note":"n1"},`+
			`{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[1]+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	// Fourth wallet: registry row only (no period metrics) for the null case.
	bare := fmt.Sprintf("0x71ade%035x", 99)
	if _, err := s.db.Exec(context.Background(), `
		INSERT INTO trader_registry (venue_id, wallet_address, discovery_source)
		VALUES ($1, $2, 'manual')`, s.venueID, bare); err != nil {
		t.Fatalf("bare registry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.db.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, bare)
	})
	code, _ = s.doJSON(t, "POST", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+bare+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("add bare: %d", code)
	}
	code, resp = s.doJSON(t, "GET", "/api/v1/trader-groups/"+gid+"/members", userA, "")
	if code != http.StatusOK {
		t.Fatalf("members: %d %v", code, resp)
	}
	rows := rowsOf(t, resp)
	if len(rows) != 3 {
		t.Fatalf("members: %v", rows)
	}
	byAddr := map[string]map[string]any{}
	for _, r := range rows {
		m := r.(map[string]any)
		byAddr[m["wallet_address"].(string)] = m
	}
	m0 := byAddr[s.addrs[0]]
	if m0["alias"] != "w1" || m0["note"] != "n1" {
		t.Errorf("alias/note: %v", m0)
	}
	met, ok := m0["metrics"].(map[string]any)
	if !ok || met["pnl"] == nil {
		t.Errorf("metrics present with pnl: %v", m0["metrics"])
	}
	if byAddr[bare]["metrics"] != nil {
		t.Errorf("metrics null without data: %v", byAddr[bare])
	}

	// PATCH: rename alias + clear note on the same member (both change one row).
	code, resp = s.doJSON(t, "PATCH", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[0]+`","alias":"w1x","note":""}]}`)
	if code != http.StatusOK {
		t.Fatalf("patch: %d %v", code, resp)
	}
	if resp["data"].(map[string]any)["updated"] != float64(1) {
		t.Errorf("updated count (one changed row): %v", resp)
	}
	code, resp = s.doJSON(t, "GET", "/api/v1/trader-groups/"+gid+"/members", userA, "")
	rows = rowsOf(t, resp)
	byAddr = map[string]map[string]any{}
	for _, r := range rows {
		m := r.(map[string]any)
		byAddr[m["wallet_address"].(string)] = m
	}
	if byAddr[s.addrs[0]]["alias"] != "w1x" {
		t.Errorf("alias patched: %v", byAddr[s.addrs[0]])
	}
	if byAddr[s.addrs[0]]["note"] != nil {
		t.Errorf("empty clears to null: %v", byAddr[s.addrs[0]])
	}

	// Idempotent rerun: same values → 0 changed.
	code, resp = s.doJSON(t, "PATCH", "/api/v1/trader-groups/"+gid+"/members", userA,
		`{"members":[{"venue":"trader-e2e-venue","wallet_address":"`+s.addrs[0]+`","alias":"w1x"}]}`)
	if code != http.StatusOK || resp["data"].(map[string]any)["updated"] != float64(0) {
		t.Errorf("no-op rerun: %d %v", code, resp)
	}

	// Period scoping: only 30D seeded → 7D metrics null.
	code, resp = s.doJSON(t, "GET", "/api/v1/trader-groups/"+gid+"/members?period=7D", userA, "")
	if code != http.StatusOK {
		t.Fatalf("period: %d %v", code, resp)
	}
	for _, r := range rowsOf(t, resp) {
		if r.(map[string]any)["metrics"] != nil {
			t.Errorf("7D metrics must be null: %v", r)
		}
	}
}

// PG-E-01/PG-E-02: numbered walk over HTTP — stateless deep link first
// (page 2 with no prior cursor state), then page 1, then beyond-total.
// Each page REPLACES rows; meta.page/total_pages guide the client.
func TestE2E_Trader_NumberedPagination(t *testing.T) {
	s := setupTraderSuite(t)
	userA := s.userA.String()

	search := func(page int) (int, map[string]any) {
		return s.doJSON(t, "POST", "/api/v1/traders/search", userA,
			`{"period":"30D","venue":"trader-e2e-venue","sort_by":"pnl","sort_direction":"desc","limit":2,"page":`+
				fmt.Sprintf("%d", page)+`}`)
	}

	// PG-E-02 first: page 2 as the very first request (no cursor ever issued).
	code, resp := search(2)
	if code != http.StatusOK {
		t.Fatalf("page 2: %d %v", code, resp)
	}
	rows := rowsOf(t, resp)
	if len(rows) != 1 {
		t.Fatalf("page 2 of 3/limit 2: want 1 row, got %v", rows)
	}
	meta := resp["meta"].(map[string]any)
	if meta["page"] != 2.0 || meta["total"] != 3.0 || meta["total_pages"] != 2.0 {
		t.Fatalf("meta: want page=2 total=3 total_pages=2, got %v", meta)
	}
	if meta["has_more"] != false {
		t.Fatalf("last page: has_more must be false: %v", meta)
	}
	page2Addr := rows[0].(map[string]any)["wallet_address"].(string)

	// PG-E-01: page 1 holds the other two rows (replace, never append).
	code, resp = search(1)
	if code != http.StatusOK {
		t.Fatalf("page 1: %d %v", code, resp)
	}
	rows = rowsOf(t, resp)
	if len(rows) != 2 {
		t.Fatalf("page 1: want 2 rows, got %v", rows)
	}
	for _, r := range rows {
		if r.(map[string]any)["wallet_address"].(string) == page2Addr {
			t.Fatalf("page 1 overlaps page 2: %v", rows)
		}
	}
	if meta = resp["meta"].(map[string]any); meta["has_more"] != true {
		t.Fatalf("page 1: has_more must be true: %v", meta)
	}

	// Beyond total → empty data, has_more=false.
	code, resp = search(3)
	if code != http.StatusOK {
		t.Fatalf("page 3: %d %v", code, resp)
	}
	if len(rowsOf(t, resp)) != 0 {
		t.Fatalf("beyond total: want empty, got %v", resp)
	}
	if meta = resp["meta"].(map[string]any); meta["has_more"] != false {
		t.Fatalf("beyond total: has_more must be false: %v", meta)
	}
}

// E2E-T-03: fake WS trade feed → harvest → registry → detail shows
// source=ws_trade with null metrics.
func TestE2E_Trader_WSdiscovery(t *testing.T) {
	s := setupTraderSuite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := traderRandAddr(t)
	t.Cleanup(func() {
		_, _ = s.db.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, addr)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Method       string `json:"method"`
				Subscription struct {
					Coin string `json:"coin"`
				} `json:"subscription"`
			}
			if json.Unmarshal(raw, &msg) != nil || msg.Method != "subscribe" {
				continue
			}
			frame := fmt.Sprintf(`{"channel":"trades","data":[{"coin":"BTC","side":"B",`+
				`"px":"1","sz":"1","time":%d,"hash":"h","tid":1,"users":["%s","%s"]}]}`,
				time.Now().UTC().UnixMilli(), addr, addr)
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(json.RawMessage(frame))
		}
	}))
	defer srv.Close()

	repo := trader.NewRepository(s.db)
	harvest := trader.NewWSHarvestService(repo, s.venueID, 100, 50*time.Millisecond)
	stream := hyperliquid.NewTradeStream("ws"+strings.TrimPrefix(srv.URL, "http"), 10,
		func(events []hyperliquid.WSTradeEvent) { harvest.Submit(trader.AdaptWSBatch(events)) })
	if err := stream.SetCoins([]string{"BTC"}); err != nil {
		t.Fatalf("coins: %v", err)
	}
	go harvest.Start(ctx)
	go func() { _ = stream.Run(ctx) }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		var source string
		_ = s.db.QueryRow(ctx, `SELECT discovery_source FROM trader_registry
			WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, addr).Scan(&source)
		if source == "ws_trade" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ws wallet never harvested (source=%q)", source)
		}
		time.Sleep(200 * time.Millisecond)
	}

	code, resp := s.doJSON(t, "GET", "/api/v1/traders/"+addr+"?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["registry"].(map[string]any)["discovery_source"] != "ws_trade" {
		t.Errorf("source: %v", data)
	}
	if data["metrics"] != nil {
		t.Errorf("never-synced wallet must have null metrics: %v", data)
	}
}

// E2E-T-05 (M7): positions flow — seed registry + sync-state +
// positions/summary → GET positions 200; unknown wallet 404.
func TestE2E_Trader_PositionsFlow(t *testing.T) {
	s := setupTraderSuite(t)
	ctx := context.Background()
	repo := trader.NewRepository(s.db)
	addr := traderRandAddr(t)
	t.Cleanup(func() {
		_, _ = s.db.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, addr)
	})

	if _, _, err := repo.UpsertRegistry(ctx, s.venueID, addr, trader.SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	now := time.Now().UTC()
	av, nt, mu := 12345.6, 5000.0, 800.0
	snap := &trader.PositionSnapshot{
		Positions: []trader.Position{
			{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: &av},
		},
		AccountValue: &av, TotalNtlPos: &nt, TotalMarginUsed: &mu, AsOf: now,
	}
	if err := repo.ReplacePositions(ctx, s.venueID, addr, snap); err != nil {
		t.Fatalf("seed positions: %v", err)
	}
	if _, err := repo.EnsureSyncState(ctx, s.venueID, addr); err != nil {
		t.Fatalf("sync state: %v", err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE trader_sync_state SET last_positions_sync_at=$3
		WHERE venue_id=$1 AND wallet_address=$2`, s.venueID, addr, now); err != nil {
		t.Fatalf("sync timestamp: %v", err)
	}

	code, resp := s.doJSON(t, "GET", "/api/v1/traders/"+addr+"/positions?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("positions: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["data_status"] != "ready" {
		t.Errorf("data_status: %v", data)
	}
	if data["summary"] == nil {
		t.Errorf("summary must be present: %v", data)
	}
	rows, ok := data["positions"].([]any)
	if !ok || len(rows) != 1 || rows[0].(map[string]any)["coin"] != "BTC" {
		t.Errorf("positions rows: %v", data["positions"])
	}

	unknown := traderRandAddr(t)
	code, _ = s.doJSON(t, "GET", "/api/v1/traders/"+unknown+"/positions?venue=trader-e2e-venue", "", "")
	if code != http.StatusNotFound {
		t.Errorf("unknown wallet must 404, got %d", code)
	}
}

// E2E-T-06 (M7): activity flow — seed 3 closed trades → GET activity limit=2
// → follow next_cursor; net_pnl correct; bad cursor 400.
func TestE2E_Trader_ActivityFlow(t *testing.T) {
	s := setupTraderSuite(t)
	ctx := context.Background()
	repo := trader.NewRepository(s.db)
	addr := traderRandAddr(t)
	t.Cleanup(func() {
		_, _ = s.db.Exec(context.Background(),
			`DELETE FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2`, s.venueID, addr)
	})

	if _, _, err := repo.UpsertRegistry(ctx, s.venueID, addr, trader.SourceLeaderboard, nil, nil); err != nil {
		t.Fatalf("registry: %v", err)
	}
	day1 := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if err := repo.ReplaceTradesForDay(ctx, s.venueID, addr, day1, []trader.CompletedTrade{
		{Market: "SOL", Long: true, OpenTime: day1.Add(time.Hour), CloseTime: day1.Add(2 * time.Hour), Volume: 1000, PnL: 50, Fees: 2, Fills: 1},
	}); err != nil {
		t.Fatalf("day1: %v", err)
	}
	if err := repo.ReplaceTradesForDay(ctx, s.venueID, addr, day2, []trader.CompletedTrade{
		{Market: "BTC", Long: true, OpenTime: day2.Add(5 * time.Hour), CloseTime: day2.Add(6 * time.Hour), Volume: 30000, PnL: 1500, Fees: 30, Fills: 3},
		{Market: "ETH", Long: false, OpenTime: day2.Add(time.Hour), CloseTime: day2.Add(2 * time.Hour), Volume: 5000, PnL: -100, Fees: 5, Fills: 2},
	}); err != nil {
		t.Fatalf("day2: %v", err)
	}

	code, resp := s.doJSON(t, "GET", "/api/v1/traders/"+addr+"/activity?venue=trader-e2e-venue&limit=2", "", "")
	if code != http.StatusOK {
		t.Fatalf("activity p1: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	rows, ok := data["rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("page 1 rows: %v", data)
	}
	if data["has_more"] != true || data["next_cursor"] == nil || data["next_cursor"] == "" {
		t.Fatalf("page 1 pagination: %v", data)
	}
	if rows[0].(map[string]any)["market"] != "BTC" || rows[0].(map[string]any)["net_pnl"] != 1470.0 {
		t.Errorf("page 1 content: %v", rows)
	}
	cursor := data["next_cursor"].(string)

	code, resp = s.doJSON(t, "GET", "/api/v1/traders/"+addr+"/activity?venue=trader-e2e-venue&limit=2&cursor="+cursor, "", "")
	if code != http.StatusOK {
		t.Fatalf("activity p2: %d %v", code, resp)
	}
	data = resp["data"].(map[string]any)
	rows, ok = data["rows"].([]any)
	if !ok || len(rows) != 1 || rows[0].(map[string]any)["market"] != "SOL" {
		t.Errorf("page 2 rows: %v", data)
	}
	if data["has_more"] != false {
		t.Errorf("page 2 has_more: %v", data)
	}

	code, _ = s.doJSON(t, "GET", "/api/v1/traders/"+addr+"/activity?venue=trader-e2e-venue&cursor=forged.cursor", "", "")
	if code != http.StatusBadRequest {
		t.Errorf("bad cursor must 400, got %d", code)
	}
}
