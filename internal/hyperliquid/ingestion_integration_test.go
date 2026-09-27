//go:build integration

package hyperliquid

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
)

type ingFixture struct {
	pool    *pgxpool.Pool
	fills   *wallet.FillRepository
	venueID uuid.UUID
	addrs   []string
}

func setupIngFixture(t *testing.T) *ingFixture {
	t.Helper()

	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping (is docker running?): %v", err)
	}

	f := &ingFixture{pool: pool, fills: wallet.NewFillRepository(pool)}
	if err := pool.QueryRow(ctx, `SELECT id FROM venues WHERE code = 'hyperliquid'`).Scan(&f.venueID); err != nil {
		pool.Close()
		t.Fatalf("hyperliquid venue (migrate up?): %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM wallet_metric_snapshots WHERE wallet_id IN (
			SELECT id FROM tracked_wallets WHERE address = ANY($1))`, f.addrs)
		pool.Exec(ctx, `DELETE FROM wallet_fills WHERE wallet_id IN (
			SELECT id FROM tracked_wallets WHERE address = ANY($1))`, f.addrs)
		pool.Exec(ctx, `DELETE FROM tracked_wallets WHERE address = ANY($1)`, f.addrs)
		pool.Close()
	})
	return f
}

func (f *ingFixture) addWallet(t *testing.T, i int) (uuid.UUID, string) {
	t.Helper()
	addr := fmt.Sprintf("0x%040x", 0x9000+i)
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

type fakeVenue struct {
	fills     []Fill
	truncated bool
}

func (f *fakeVenue) FetchAll(ctx context.Context, address string, startMs, endMs int64) ([]Fill, bool, error) {
	var out []Fill
	for _, fl := range f.fills {
		if fl.Time >= startMs && fl.Time < endMs {
			out = append(out, fl)
		}
	}
	return out, f.truncated, nil
}

func snapshot(t *testing.T, f *ingFixture, walletID uuid.UUID, timeframe, market string) (tradeCount *int64, partial bool, found bool) {
	t.Helper()
	var tc *int64
	var isPartial bool
	err := f.pool.QueryRow(context.Background(), `
		SELECT trade_count, is_partial FROM wallet_metric_snapshots
		WHERE wallet_id = $1 AND timeframe = $2 AND venue_id IS NULL
		  AND ((market IS NULL AND $3::text IS NULL) OR market = $3)`,
		walletID, timeframe, nilString(market)).Scan(&tc, &isPartial)
	if err != nil {
		return nil, false, false
	}
	return tc, isPartial, true
}

func nilString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func tcValue(tc *int64) any {
	if tc == nil {
		return nil
	}
	return *tc
}

// ING-U-05: same fill delivered twice → 1 row.
func TestFillRepo_UpsertFills_Idempotent(t *testing.T) {
	f := setupIngFixture(t)
	ctx := context.Background()
	walletID, _ := f.addWallet(t, 1)

	inputs, skipped := NormalizeFills([]Fill{
		fill("BTC", "Open Long", "0", "100", "1", "0", "0.1", "B", ms("2026-09-01T10:00:00Z"), 11),
	})
	if skipped != 0 || len(inputs) != 1 {
		t.Fatalf("fixture: %+v skipped=%d", inputs, skipped)
	}

	n, err := f.fills.UpsertFills(ctx, walletID, f.venueID, inputs)
	if err != nil || n != 1 {
		t.Fatalf("first upsert: n=%d err=%v", n, err)
	}
	n, err = f.fills.UpsertFills(ctx, walletID, f.venueID, inputs)
	if err != nil || n != 0 {
		t.Fatalf("second upsert: n=%d err=%v", n, err)
	}

	var count int64
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_fills WHERE wallet_id = $1`, walletID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

// ING-I-01: backfill writes snapshots for every timeframe window.
func TestBackfill_WalletWithHistory_AllTimeframes(t *testing.T) {
	f := setupIngFixture(t)
	ctx := context.Background()
	walletID, addr := f.addWallet(t, 2)
	now := time.Now().UTC()

	at := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	venue := &fakeVenue{fills: []Fill{
		fill("BTC", "Open Long", "0", "100", "1", "0", "0", "B", at(time.Hour), 101),
		fill("BTC", "Open Long", "0", "100", "1", "5", "0", "B", at(3*24*time.Hour), 102),
		fill("BTC", "Open Long", "0", "100", "1", "5", "0", "B", at(20*24*time.Hour), 103),
		fill("BTC", "Open Long", "0", "100", "1", "5", "0", "B", at(60*24*time.Hour), 104),
		fill("BTC", "Open Long", "0", "100", "1", "5", "0", "B", at(200*24*time.Hour), 105),
	}}

	svc := NewBackfillService(f.fills, venue, f.venueID)
	if err := svc.BackfillWallet(ctx, walletID, addr, now); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	for tf, want := range map[string]int64{
		wallet.Timeframe24H: 1, wallet.Timeframe7D: 2, wallet.Timeframe30D: 3,
		wallet.Timeframe90D: 4, wallet.TimeframeALL: 5,
	} {
		tc, partial, found := snapshot(t, f, walletID, tf, "")
		if !found {
			t.Errorf("%s: no aggregate snapshot", tf)
			continue
		}
		if tc == nil || *tc != want {
			t.Errorf("%s: expected %d trades, got %v", tf, want, tcValue(tc))
		}
		if partial {
			t.Errorf("%s: unexpected partial flag", tf)
		}
	}

	// Per-market snapshot exists too.
	if _, _, found := snapshot(t, f, walletID, wallet.Timeframe30D, "BTC"); !found {
		t.Error("30D: no per-market BTC snapshot")
	}
}

// ING-I-02: engine output matches hand-computed fixture values.
func TestBackfill_EngineMatchesFixture(t *testing.T) {
	f := setupIngFixture(t)
	ctx := context.Background()
	walletID, addr := f.addWallet(t, 3)
	now := time.Now().UTC()

	// One leg: open 1 BTC @100, close half +5, close rest +10, no fees.
	// pnl = 15, volume = 100 + 55 + 60 = 215, trades = 1, win = 100%.
	venue := &fakeVenue{fills: []Fill{
		fill("BTC", "Open Long", "0", "100", "1", "0", "0", "B", now.Add(-3*time.Hour).UnixMilli(), 201),
		fill("BTC", "Close Long", "1", "110", "0.5", "5", "0", "A", now.Add(-2*time.Hour).UnixMilli(), 202),
		fill("BTC", "Close Long", "0.5", "120", "0.5", "10", "0", "A", now.Add(-time.Hour).UnixMilli(), 203),
	}}

	svc := NewBackfillService(f.fills, venue, f.venueID)
	if err := svc.BackfillWallet(ctx, walletID, addr, now); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var pnl, roi, win, vol *float64
	var trades *int64
	err := f.pool.QueryRow(ctx, `
		SELECT realized_pnl, roi, win_rate, volume, trade_count
		FROM wallet_metric_snapshots
		WHERE wallet_id = $1 AND timeframe = '30D' AND venue_id IS NULL AND market IS NULL`,
		walletID).Scan(&pnl, &roi, &win, &vol, &trades)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if pnl == nil || *pnl != 15 {
		t.Errorf("pnl: expected 15, got %v", pnl)
	}
	if vol == nil || *vol != 215 {
		t.Errorf("volume: expected 215, got %v", vol)
	}
	if trades == nil || *trades != 1 {
		t.Errorf("trades: expected 1, got %v", trades)
	}
	if win == nil || *win != 100 {
		t.Errorf("win_rate: expected 100, got %v", win)
	}
	if roi == nil || !near(*roi, 15.0/215.0*100) {
		t.Errorf("roi: expected %v, got %v", 15.0/215.0*100, roi)
	}
}

func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// ING-I-03a: an always-full venue terminates at the subdivision floor and
// flags truncated (no infinite loop).
func TestClient_FetchAll_AlwaysFull_TerminatesTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req fillsByTimeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		fills := make([]Fill, maxFillsPerResponse)
		for i := range fills {
			fills[i] = Fill{
				Coin: "BTC", Px: "100", Sz: "0.01", Side: "B",
				Time: req.StartTime + int64(i%1000), StartPosition: "0",
				Dir: "Open Long", ClosedPnl: "0", Fee: "0",
				Tid: req.StartTime*100003 + int64(i),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fills)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0, 0)
	done := make(chan struct{})
	var got []Fill
	var truncated bool
	var ferr error
	go func() {
		defer close(done)
		got, truncated, ferr = c.FetchAll(context.Background(), "0xabc", 0, 3*3_600_000)
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("FetchAll did not terminate (infinite subdivision?)")
	}
	if ferr != nil {
		t.Fatalf("FetchAll: %v", ferr)
	}
	if !truncated {
		t.Error("always-full min-slices must flag truncated")
	}
	if len(got) == 0 {
		t.Error("expected fills from full pages")
	}
}

// ING-I-03b: a truncated window marks the snapshot is_partial.
func TestBackfill_TruncatedWindow_MarksPartial(t *testing.T) {
	f := setupIngFixture(t)
	ctx := context.Background()
	walletID, addr := f.addWallet(t, 4)
	now := time.Now().UTC()

	venue := &fakeVenue{
		fills: []Fill{
			fill("BTC", "Open Long", "0", "100", "1", "3", "0", "B", now.Add(-time.Hour).UnixMilli(), 301),
		},
		truncated: true,
	}
	svc := NewBackfillService(f.fills, venue, f.venueID)
	if err := svc.BackfillWallet(ctx, walletID, addr, now); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	_, partial, found := snapshot(t, f, walletID, wallet.Timeframe30D, "")
	if !found {
		t.Fatal("no 30D snapshot")
	}
	if !partial {
		t.Error("expected is_partial=true")
	}
}
