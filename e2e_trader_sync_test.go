//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
	"github.com/qwerty7415963/go_be_arbitrage/internal/tradergroup"
)

type e2eFakeFills struct {
	fills []trader.Fill
}

func (f *e2eFakeFills) FetchTraderFills(_ context.Context, _ string, _, _ int64) ([]trader.Fill, bool, error) {
	return f.fills, false, nil
}

// TestE2E_Trader_SyncFlow: seed unsynced wallet ⇒ GET activity syncing ⇒
// POST /sync twice fast (queued + in_flight, one pass) ⇒ drain ⇒ activity
// ready with rows ⇒ POST again recent. Unknown wallet 404, bad address 400.
func TestE2E_Trader_SyncFlow(t *testing.T) {
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
	d1 := now.Add(-48 * time.Hour).Truncate(24 * time.Hour)
	fills := []trader.Fill{
		{Market: "BTC", Tid: 1, FilledAt: d1.Add(2 * time.Hour), Buy: true, Quantity: 1, Price: 100, Fee: 0.1},
		{Market: "BTC", Tid: 2, FilledAt: d1.Add(3 * time.Hour), Buy: false, Quantity: 1, Price: 110, ClosedPnL: 10, Fee: 0.1},
	}
	syncSvc := trader.NewSyncService(repo, &e2eFakeFills{fills: fills}, s.venueID, trader.DefaultSyncOptions())
	svc := trader.NewService(repo, tradergroup.NewRepository(s.db), []byte("e2e-cursor-secret-32-bytes!!!!!"))
	h := trader.NewHandler(svc).WithSync(syncSvc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	h.RegisterRoutes(v1)
	tradergroup.NewHandler(tradergroup.NewRepository(s.db)).RegisterRoutes(v1)
	s.router = router
	do := func(method, target, body string) (int, map[string]any) {
		return s.doJSON(t, method, target, "", body)
	}

	// Before sync: activity syncing with empty rows (never definitive empty).
	code, resp := do("GET", "/api/v1/traders/"+addr+"/activity?venue=trader-e2e-venue", "")
	if code != http.StatusOK {
		t.Fatalf("activity pre-sync: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["data_status"] != "syncing" {
		t.Fatalf("pre-sync must be syncing: %v", data)
	}
	if rows := data["rows"].([]any); len(rows) != 0 {
		t.Fatalf("pre-sync rows empty: %v", rows)
	}

	// Trigger twice fast: first queued, second in_flight (one pass).
	code, resp = do("POST", "/api/v1/traders/"+addr+"/sync?venue=trader-e2e-venue", "")
	if code != http.StatusAccepted {
		t.Fatalf("sync first: %d %v", code, resp)
	}
	if resp["data"].(map[string]any)["status"] != "queued" {
		t.Fatalf("first must be queued: %v", resp)
	}
	code, resp = do("POST", "/api/v1/traders/"+addr+"/sync?venue=trader-e2e-venue", "")
	if code != http.StatusAccepted {
		t.Fatalf("sync second: %d %v", code, resp)
	}
	if resp["data"].(map[string]any)["status"] != "in_flight" {
		t.Fatalf("second must be in_flight: %v", resp)
	}

	// Unknown wallet 404, bad address 400.
	unknown := traderRandAddr(t)
	if code, _ := do("POST", "/api/v1/traders/"+unknown+"/sync?venue=trader-e2e-venue", ""); code != http.StatusNotFound {
		t.Errorf("unknown wallet must 404, got %d", code)
	}
	if code, _ := do("POST", "/api/v1/traders/not-an-address/sync?venue=trader-e2e-venue", ""); code != http.StatusBadRequest {
		t.Errorf("bad address must 400, got %d", code)
	}

	// Drain: one priority pass runs without manual refresh.
	ctxDrain, cancel := context.WithCancel(context.Background())
	defer cancel()
	go syncSvc.StartPriority(ctxDrain)
	deadline := time.Now().Add(15 * time.Second)
	for {
		var completed *time.Time
		_ = s.db.QueryRow(ctx, `SELECT backfill_completed_at FROM trader_sync_state
			WHERE venue_id=$1 AND wallet_address=$2`, s.venueID, addr).Scan(&completed)
		if completed != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("priority drain never completed")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// After drain: activity ready (rows appear, no manual refresh needed).
	code, resp = do("GET", "/api/v1/traders/"+addr+"/activity?venue=trader-e2e-venue", "")
	if code != http.StatusOK {
		t.Fatalf("activity post-sync: %d %v", code, resp)
	}
	data = resp["data"].(map[string]any)
	if data["data_status"] != "ready" {
		t.Errorf("post-sync must be ready: %v", data)
	}
	if rows := data["rows"].([]any); len(rows) == 0 {
		t.Errorf("post-sync rows must appear: %v", data)
	}

	// Debounce: fresh sync ⇒ recent, no new pass. The drain may still be
	// finishing its pass (backfill_completed_at is set mid-pass, before the
	// inflight mark clears), so poll until `recent`, tolerating `in_flight`.
	deadline2 := time.Now().Add(15 * time.Second)
	for {
		code, resp = do("POST", "/api/v1/traders/"+addr+"/sync?venue=trader-e2e-venue", "")
		if code != http.StatusAccepted {
			t.Fatalf("debounce status: %d %v", code, resp)
		}
		if st := resp["data"].(map[string]any)["status"]; st == "recent" {
			break
		} else if st != "in_flight" {
			t.Fatalf("debounce must settle at recent (via in_flight): %d %v", code, resp)
		}
		if time.Now().After(deadline2) {
			t.Fatalf("debounce never recent, stuck in_flight: %d %v", code, resp)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
