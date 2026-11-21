//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

func makeE2EPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func strPtr(s string) *string { return &s }

func TestE2E_AssetLogo(t *testing.T) {
	s := setupTraderSuite(t)
	ctx := context.Background()
	repo := trader.NewRepository(s.db)

	// seed period metrics + positions for wallets
	pnls := []float64{5000, 15000, 25000}
	vols := []float64{1000, 2000, 3000}
	for i, addr := range s.addrs {
		pnl := pnls[i]
		vol := vols[i]
		tc := int64(4 + i)
		lc := int64(2 + i)
		sc := int64(1)
		if err := repo.UpsertPeriodMetrics(ctx, &trader.PeriodMetrics{
			VenueID: s.venueID, WalletAddress: addr, Period: trader.Period30D,
			AsOf: time.Now().UTC(), PnL: &pnl, RealizedPnL: &pnl,
			Volume: &vol, TradeCount: &tc, LongCount: &lc, ShortCount: &sc,
			DataStatus: trader.DataReady, CalculationVersion: 1,
		}); err != nil {
			t.Fatalf("period: %v", err)
		}
		eq := pnl * 10
		day := time.Now().UTC().Truncate(24 * time.Hour)
		if err := repo.UpsertEquityDaily(ctx, s.venueID, addr, trader.EquityDay{
			Date: day, StartEquity: &eq, EndEquity: &eq, PeakEquity: &eq,
		}); err != nil {
			t.Fatalf("equity: %v", err)
		}
		snap := &trader.PositionSnapshot{
			Positions: []trader.Position{
				{Coin: "BTC", Side: "LONG", Size: 0.5, EntryPrice: fptr64(60000), MarkPrice: fptr64(63000), PositionValue: fptr64(31500)},
			},
			AccountValue:    &eq,
			TotalNtlPos:     &vol,
			TotalMarginUsed: fptr64(800),
			AsOf:            time.Now().UTC(),
		}
		if i == 1 {
			snap.Positions = []trader.Position{
				{Coin: "ETH", Side: "LONG", Size: 1, EntryPrice: fptr64(3000), MarkPrice: fptr64(3100), PositionValue: fptr64(3100)},
			}
		}
		if err := repo.ReplacePositions(ctx, s.venueID, addr, snap); err != nil {
			t.Fatalf("positions: %v", err)
		}
	}

	// clean leftover aliases from prior runs
	_, _ = s.db.Exec(ctx, `DELETE FROM asset_aliases WHERE venue_id = $1 AND venue_symbol IN ('BTC','ETH')`, s.venueID)

	// seed assets + aliases + logos
	alRepo := s.assetLogoRepo
	readyAsset := &assetlogo.Asset{CanonicalKey: "crypto:BTC", Symbol: "BTC", AssetClass: assetlogo.AssetClassCryptoNative, Metadata: json.RawMessage("{}")}
	if err := alRepo.CreateAsset(ctx, readyAsset); err != nil {
		t.Fatalf("asset ready: %v", err)
	}
	readyAlias := &assetlogo.AssetAlias{AssetID: readyAsset.ID, VenueID: &s.venueID, VenueSymbol: "BTC"}
	if err := alRepo.CreateAlias(ctx, readyAlias); err != nil {
		t.Fatalf("alias ready: %v", err)
	}
	pngBytes := makeE2EPNG()
	readyPath, err := s.assetLogoStorage.Put(ctx, readyAsset.ID.String(), pngBytes, "image/png")
	if err != nil {
		t.Fatalf("storage put: %v", err)
	}
	readyCT := "image/png"
	readyETag := "etag-ready"
	if err := alRepo.UpsertLogo(ctx, &assetlogo.AssetLogo{
		AssetID: readyAsset.ID, Provider: "trustwallet", Status: assetlogo.LogoStatusFound,
		StoragePath: &readyPath, ContentType: &readyCT, ETag: &readyETag,
	}); err != nil {
		t.Fatalf("logo ready: %v", err)
	}

	missingAsset := &assetlogo.Asset{CanonicalKey: "crypto:ETH", Symbol: "ETH", AssetClass: assetlogo.AssetClassCryptoNative, Metadata: json.RawMessage("{}")}
	if err := alRepo.CreateAsset(ctx, missingAsset); err != nil {
		t.Fatalf("asset missing: %v", err)
	}
	missingAlias := &assetlogo.AssetAlias{AssetID: missingAsset.ID, VenueID: &s.venueID, VenueSymbol: "ETH"}
	if err := alRepo.CreateAlias(ctx, missingAlias); err != nil {
		t.Fatalf("alias missing: %v", err)
	}
	missingStatus := assetlogo.LogoStatusMissing
	if err := alRepo.UpsertLogo(ctx, &assetlogo.AssetLogo{
		AssetID: missingAsset.ID, Provider: "trustwallet", Status: missingStatus,
	}); err != nil {
		t.Fatalf("logo missing: %v", err)
	}

	// bucket detail
	code, resp := s.doJSON(t, "GET", "/v1/traders/overview/buckets/pnl/profitable?venue=trader-e2e-venue", "", "")
	if code != http.StatusOK {
		t.Fatalf("bucket detail: %d %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	markets, _ := data["markets_top"].([]any)
	if len(markets) == 0 {
		t.Fatalf("markets_top empty")
	}
	var btcMarket, ethMarket map[string]any
	for _, m := range markets {
		mm := m.(map[string]any)
		if mm["market"] == "BTC" {
			btcMarket = mm
		}
		if mm["market"] == "ETH" {
			ethMarket = mm
		}
	}
	if btcMarket == nil {
		t.Fatalf("BTC market missing: %v", markets)
	}
	if btcMarket["asset_id"] != readyAsset.ID.String() {
		t.Errorf("btc asset_id: %v want %v", btcMarket["asset_id"], readyAsset.ID)
	}
	wantLogo := "/v1/assets/logos/" + readyAsset.ID.String()
	if btcMarket["logo_url"] != wantLogo {
		t.Errorf("btc logo_url: %v want %v", btcMarket["logo_url"], wantLogo)
	}
	if ethMarket != nil {
		lu := ethMarket["logo_url"]
		if lu != "" && lu != nil {
			t.Errorf("eth logo_url: %v want empty/null", lu)
		}
	}

	// serve route ready -> 200 with cache headers + etag
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/assets/logos/"+readyAsset.ID.String(), nil)
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("serve ready: %d %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("cache-control: %q", cc)
	}
	if et := w.Header().Get("ETag"); et != readyETag {
		t.Errorf("etag: %q want %q", et, readyETag)
	}
	// conditional request
	req2, _ := http.NewRequest("GET", "/v1/assets/logos/"+readyAsset.ID.String(), nil)
	req2.Header.Set("If-None-Match", readyETag)
	w2 := httptest.NewRecorder()
	s.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Errorf("conditional: %d", w2.Code)
	}

	// missing serve -> 404
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/v1/assets/logos/"+missingAsset.ID.String(), nil)
	s.router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("serve missing: %d", w3.Code)
	}

	// admin refresh -> 200 with enqueued count
	body, _ := json.Marshal(map[string]any{"all": true})
	req4, _ := http.NewRequest("POST", "/v1/admin/assets/logos/refresh", bytes.NewReader(body))
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("X-Test-User", "admin")
	req4.Header.Set("X-Test-Role", "admin")
	w4 := httptest.NewRecorder()
	s.router.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", w4.Code, w4.Body.String())
	}
	var refreshResp map[string]any
	if err := json.Unmarshal(w4.Body.Bytes(), &refreshResp); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	rdata, _ := refreshResp["data"].(map[string]any)
	if _, ok := rdata["enqueued"]; !ok {
		t.Errorf("refresh missing enqueued: %v", refreshResp)
	}
}
