//go:build integration

package assetlogo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	assetlogo "github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
	providers "github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo/providers"
)

func setupIntegrationDB4(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@localhost:5433/arbitrage_test?sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM asset_logos")
	pool.Exec(ctx, "DELETE FROM asset_provider_refs")
	pool.Exec(ctx, "DELETE FROM asset_aliases")
	pool.Exec(ctx, "DELETE FROM assets")
	return pool
}

func TestIntegration_Resolver_LogoDev(t *testing.T) {
	pool := setupIntegrationDB4(t)
	defer pool.Close()
	repo := assetlogo.NewRepository(pool)
	ctx := context.Background()

	imgBytes := makePNGIntegration()

	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgBytes)
	}))
	defer dlSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"data": map[string]string{"url": dlSrv.URL + "/logo.png", "etag": "e1"}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer apiSrv.Close()

	asset := &assetlogo.Asset{CanonicalKey: "equity:NASDAQ:AAPL", Symbol: "AAPL", AssetClass: assetlogo.AssetClassEquity, Metadata: json.RawMessage("{}")}
	if err := repo.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	refBytes, _ := json.Marshal(map[string]string{"ticker": "AAPL"})
	if err := repo.UpsertProviderRef(ctx, &assetlogo.AssetProviderRef{AssetID: asset.ID, Provider: "logodev", Ref: refBytes}); err != nil {
		t.Fatal(err)
	}

	p := &providers.LogoDevProvider{SecretKey: "sk_x", BaseURL: apiSrv.URL, Client: http.DefaultClient}
	cache := assetlogo.NewCache()
	storage := assetlogo.NewLocalStorage(t.TempDir())
	resolver := assetlogo.NewResolver(repo, storage, cache, p)

	logo, err := resolver.ResolveSync(ctx, asset.ID)
	if err != nil {
		t.Fatalf("resolve sync: %v", err)
	}
	if logo.Status != assetlogo.LogoStatusFound {
		t.Fatalf("status %s", logo.Status)
	}
	if logo.StoragePath == nil {
		t.Fatal("no storage path")
	}
	if _, err := os.Stat(*logo.StoragePath); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	_ = uuid.New
}

func makePNGIntegration() []byte {
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
