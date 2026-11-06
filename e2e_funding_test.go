//go:build e2e

package e2e

import (
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
	"github.com/qwerty7415963/go_be_arbitrage/internal/fundingarbitrage"
)

type fundingSuite struct {
	db       *pgxpool.Pool
	router   *gin.Engine
	venueAID uuid.UUID
	venueBID uuid.UUID
	instIDs  map[string]uuid.UUID
}

func setupFundingSuite(t *testing.T) *fundingSuite {
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

	s := &fundingSuite{db: pool, instIDs: map[string]uuid.UUID{}}
	run := time.Now().UnixNano()

	// venues
	for _, code := range []string{"a", "b"} {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `INSERT INTO venues (code, name, venue_type, status) VALUES ($1, $2, 'PERP_DEX', 'ACTIVE') RETURNING id`,
			fmt.Sprintf("funding-e2e-%s-%d", code, run), fmt.Sprintf("Funding E2E %s", code)).Scan(&id)
		if err != nil {
			t.Fatalf("venue %s: %v", code, err)
		}
		if code == "a" {
			s.venueAID = id
		} else {
			s.venueBID = id
		}
	}

	// instruments T1..T5
	type inst struct {
		key  string
		base string
	}
	insts := []inst{
		{"T1", "HUMA"},
		{"T2", "ERA"},
		{"T3", "BTC"},
	}
	for _, in := range insts {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `INSERT INTO instruments (canonical_symbol, base_asset, quote_asset, instrument_type, contract_type, price_tick, quantity_step, discovery_status, trading_enabled) VALUES ($1,$2,'USDT','PERP','LINEAR',0.01,0.001,'REVIEWED',true) RETURNING id`,
			fmt.Sprintf("FND-E2E-%s-%d", in.key, run), in.base).Scan(&id)
		if err != nil {
			t.Fatalf("instrument %s: %v", in.key, err)
		}
		s.instIDs[in.key] = id
	}

	// venue_instruments ACTIVE for both venues on all instruments
	for _, vid := range []uuid.UUID{s.venueAID, s.venueBID} {
		for _, in := range insts {
			_, err := pool.Exec(ctx, `INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status) VALUES ($1,$2,$3,'ACTIVE')`,
				vid, s.instIDs[in.key], in.base+"-PERP")
			if err != nil {
				t.Fatalf("venue_instrument: %v", err)
			}
		}
	}

	now := time.Now()
	stale := now.Add(-20 * time.Minute)

	// funding helper
	ins := func(venueID, instID uuid.UUID, rate string, interval int, mark, oi string, observed time.Time) {
		_, err := pool.Exec(ctx, `INSERT INTO funding_rates (venue_id, instrument_id, observed_at, funding_rate, interval_seconds, mark_price, index_price, open_interest) VALUES ($1,$2,$3,$4,$5,$6,$6,$7)`,
			venueID, instID, observed, rate, interval, mark, oi)
		if err != nil {
			t.Fatalf("funding insert: %v", err)
		}
	}

	T := func(k string) uuid.UUID { return s.instIDs[k] }

	// T1 fresh both
	ins(s.venueAID, T("T1"), "0.0001", 28800, "100", "1000", now)
	ins(s.venueBID, T("T1"), "0.0002", 28800, "101", "2000", now)
	// T2 A only
	ins(s.venueAID, T("T2"), "0.0001", 28800, "100", "1000", now)
	// T3 stale both
	ins(s.venueAID, T("T3"), "0.0001", 28800, "100", "1000", stale)
	ins(s.venueBID, T("T3"), "0.0002", 28800, "101", "2000", stale)

	// router
	repo := fundingarbitrage.NewRepository(pool)
	cache := fundingarbitrage.NewCache(30 * time.Second)
	svc := fundingarbitrage.NewService(repo, cache)
	handler := fundingarbitrage.NewHandler(svc)

	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.GET("/funding/arbitrage", handler.GetFundingArbitrage)
	s.router = router

	// cleanup (registered after pool.Close so runs first)
	venueIDs := []uuid.UUID{s.venueAID, s.venueBID}
	instIDs := make([]uuid.UUID, 0, len(s.instIDs))
	for _, id := range s.instIDs {
		instIDs = append(instIDs, id)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(c, `DELETE FROM funding_rates WHERE venue_id = ANY($1)`, venueIDs)
		_, _ = pool.Exec(c, `DELETE FROM venue_instruments WHERE venue_id = ANY($1)`, venueIDs)
		_, _ = pool.Exec(c, `DELETE FROM instruments WHERE id = ANY($1)`, instIDs)
		_, _ = pool.Exec(c, `DELETE FROM venues WHERE id = ANY($1)`, venueIDs)
	})

	return s
}

func (s *fundingSuite) doJSON(t *testing.T, method, target string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, target, nil)
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

func TestE2E_Funding_HappyPath(t *testing.T) {
	s := setupFundingSuite(t)
	a := s.venueAID.String()
	b := s.venueBID.String()
	code, resp := s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s&refresh=true", a, b))
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	pairs := data["pairs"].([]any)
	if len(pairs) != 1 {
		t.Fatalf("pairs: %d", len(pairs))
	}
	pair := pairs[0].(map[string]any)
	if pair["venue_a"] == nil || pair["venue_b"] == nil {
		t.Fatalf("missing venues")
	}
	tokens := pair["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("tokens: %d", len(tokens))
	}
	tok := tokens[0].(map[string]any)
	if tok["symbol"] != "HUMA-PERP" {
		t.Fatalf("symbol: %v", tok["symbol"])
	}
	if tok["funding_available"] != true {
		t.Fatalf("funding_available: %v", tok["funding_available"])
	}
	if tok["is_stale"] != false {
		t.Fatalf("is_stale: %v", tok["is_stale"])
	}
	if tok["venue_a_funding_rate"] != "0.0001" {
		t.Fatalf("rate: %v", tok["venue_a_funding_rate"])
	}
	if tok["venue_a_interval_seconds"] != float64(28800) {
		t.Fatalf("interval: %v", tok["venue_a_interval_seconds"])
	}
	if tok["venue_a_oi"] != "1000" {
		t.Fatalf("oi a: %v", tok["venue_a_oi"])
	}
	if tok["venue_b_oi"] != "2000" {
		t.Fatalf("oi b: %v", tok["venue_b_oi"])
	}
	if tok["rate_1h_percent"] == nil {
		t.Fatalf("rate_1h nil")
	}
	if tok["long_venue_id"] != a {
		t.Fatalf("long: %v want %s", tok["long_venue_id"], a)
	}
	if tok["short_venue_id"] != b {
		t.Fatalf("short: %v want %s", tok["short_venue_id"], b)
	}
	meta := resp["meta"].(map[string]any)
	if meta["page"] != float64(1) {
		t.Fatalf("page: %v", meta["page"])
	}
	if meta["total_pages"] != float64(1) {
		t.Fatalf("total_pages: %v", meta["total_pages"])
	}
	if meta["has_more"] != false {
		t.Fatalf("has_more: %v", meta["has_more"])
	}
	if meta["limit"] != float64(50) {
		t.Fatalf("limit: %v", meta["limit"])
	}
}

func TestE2E_Funding_Stale(t *testing.T) {
	s := setupFundingSuite(t)
	a := s.venueAID.String()
	b := s.venueBID.String()
	// Remove fresh T1 funding so default result is empty.
	_, _ = s.db.Exec(context.Background(), `DELETE FROM funding_rates WHERE instrument_id = $1`, s.instIDs["T1"])
	// default: no tokens
	code, resp := s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s&refresh=true", a, b))
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	pairs := data["pairs"].([]any)
	pair := pairs[0].(map[string]any)
	tokens := pair["tokens"]
	if tokens != nil {
		if arr, ok := tokens.([]any); ok && len(arr) != 0 {
			t.Fatalf("expected 0 tokens default, got %d", len(arr))
		}
	}
	// include_stale
	code, resp = s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s&include_stale=true&refresh=true", a, b))
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, resp)
	}
	data = resp["data"].(map[string]any)
	pairs = data["pairs"].([]any)
	pair = pairs[0].(map[string]any)
	toks := pair["tokens"].([]any)
	if len(toks) != 1 {
		t.Fatalf("expected 1 stale token, got %d", len(toks))
	}
	if toks[0].(map[string]any)["is_stale"] != true {
		t.Fatalf("is_stale: %v", toks[0].(map[string]any)["is_stale"])
	}
}

func TestE2E_Funding_Pagination(t *testing.T) {
	s := setupFundingSuite(t)
	a := s.venueAID.String()
	b := s.venueBID.String()
	// Seed 3 tokens with funding on both venues for pagination.
	now := time.Now()
	var extraIDs []int64
	add := func(vid, iid uuid.UUID, rate string) {
		var id int64
		err := s.db.QueryRow(context.Background(), `INSERT INTO funding_rates (venue_id, instrument_id, observed_at, funding_rate, interval_seconds, mark_price, index_price, open_interest) VALUES ($1,$2,$3,$4,28800,$5,$5,$6) RETURNING id`,
			vid, iid, now, rate, "100", "1000").Scan(&id)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		extraIDs = append(extraIDs, id)
	}
	add(s.venueBID, s.instIDs["T2"], "0.0002")
	add(s.venueAID, s.instIDs["T3"], "0.0001")
	add(s.venueBID, s.instIDs["T3"], "0.0002")
	t.Cleanup(func() {
		if len(extraIDs) > 0 {
			_, _ = s.db.Exec(context.Background(), `DELETE FROM funding_rates WHERE id = ANY($1)`, extraIDs)
		}
	})
	// page1
	code, resp := s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s&limit=2&refresh=true", a, b))
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	pairs := data["pairs"].([]any)
	pair := pairs[0].(map[string]any)
	tokens := pair["tokens"].([]any)
	if len(tokens) != 2 {
		t.Fatalf("page1 tokens: %d", len(tokens))
	}
	meta := resp["meta"].(map[string]any)
	if meta["has_more"] != true {
		t.Fatalf("has_more: %v", meta["has_more"])
	}
	if meta["total_pages"] != float64(2) {
		t.Fatalf("total_pages: %v", meta["total_pages"])
	}
	// page2
	code, resp = s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s&limit=2&page=2&refresh=true", a, b))
	if code != http.StatusOK {
		t.Fatalf("status: %d %v", code, resp)
	}
	data = resp["data"].(map[string]any)
	pairs = data["pairs"].([]any)
	pair = pairs[0].(map[string]any)
	tokens = pair["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("page2 tokens: %d", len(tokens))
	}
	meta = resp["meta"].(map[string]any)
	if meta["has_more"] != false {
		t.Fatalf("has_more: %v", meta["has_more"])
	}
}

func TestE2E_Funding_Validation(t *testing.T) {
	s := setupFundingSuite(t)
	a := s.venueAID.String()
	code, _ := s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s", a))
	if code != http.StatusBadRequest {
		t.Fatalf("1 venue: %d", code)
	}
	code, _ = s.doJSON(t, "GET", fmt.Sprintf("/api/v1/funding/arbitrage?venue_id=%s,%s", a, a))
	if code != http.StatusBadRequest {
		t.Fatalf("dup: %d", code)
	}
}
