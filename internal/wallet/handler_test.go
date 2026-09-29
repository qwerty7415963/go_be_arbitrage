package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

const testUser = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

type mockRepo struct {
	scanFn      func(ctx context.Context, f *Filters, sort *SortSpec, groupID *uuid.UUID, userID uuid.UUID, limit, offset int) ([]*Wallet, int64, error)
	detailFn    func(ctx context.Context, id, userID uuid.UUID, f *Filters) (*WalletDetail, error)
	ownerFn     func(ctx context.Context, groupID uuid.UUID) (uuid.UUID, bool, error)
	existsFn    func(ctx context.Context, id uuid.UUID) (bool, error)
	upsertTagFn func(ctx context.Context, userID, walletID uuid.UUID, tag string) error
	clearTagFn  func(ctx context.Context, userID, walletID uuid.UUID) error
	watchFn     func(ctx context.Context, userID, walletID uuid.UUID, on bool) error
	lastFilters *Filters
	lastSort    *SortSpec
	lastLimit   int
	lastOffset  int
}

func (m *mockRepo) ScanWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID *uuid.UUID, userID uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
	m.lastFilters, m.lastSort, m.lastLimit, m.lastOffset = f, sort, limit, offset
	if m.scanFn != nil {
		return m.scanFn(ctx, f, sort, groupID, userID, limit, offset)
	}
	return []*Wallet{}, 0, nil
}

func (m *mockRepo) GetDetail(ctx context.Context, id, userID uuid.UUID, f *Filters) (*WalletDetail, error) {
	if m.detailFn != nil {
		return m.detailFn(ctx, id, userID, f)
	}
	return nil, errors.New("detailFn not set")
}

func (m *mockRepo) ScanGroupWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID uuid.UUID, userID uuid.UUID, limit, offset int) ([]*GroupWallet, int64, error) {
	return []*GroupWallet{}, 0, nil
}

func (m *mockRepo) GetGroupOwner(ctx context.Context, groupID uuid.UUID) (uuid.UUID, bool, error) {
	if m.ownerFn != nil {
		return m.ownerFn(ctx, groupID)
	}
	return uuid.Nil, false, errors.New("ownerFn not set")
}

func (m *mockRepo) WalletExists(ctx context.Context, id uuid.UUID) (bool, error) {
	if m.existsFn != nil {
		return m.existsFn(ctx, id)
	}
	return false, errors.New("existsFn not set")
}

func (m *mockRepo) UpsertTag(ctx context.Context, userID, walletID uuid.UUID, tag string) error {
	if m.upsertTagFn != nil {
		return m.upsertTagFn(ctx, userID, walletID, tag)
	}
	return errors.New("upsertTagFn not set")
}

func (m *mockRepo) ClearTag(ctx context.Context, userID, walletID uuid.UUID) error {
	if m.clearTagFn != nil {
		return m.clearTagFn(ctx, userID, walletID)
	}
	return errors.New("clearTagFn not set")
}

func (m *mockRepo) SetWatchlisted(ctx context.Context, userID, walletID uuid.UUID, on bool) error {
	if m.watchFn != nil {
		return m.watchFn(ctx, userID, walletID, on)
	}
	return errors.New("watchFn not set")
}

func setupRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})
	handler.RegisterRoutes(router.Group("/api/v1"))
	return router
}

func get(t *testing.T, router *gin.Engine, target, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return resp
}

func expectCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected %d, got %d: %s", status, w.Code, w.Body.String())
	}
	resp := decode(t, w)
	errObj, _ := resp["error"].(map[string]interface{})
	got, _ := errObj["code"].(string)
	if got != code {
		t.Errorf("expected error code %s, got %q (body %s)", code, got, w.Body.String())
	}
}

func sampleWallet(id uuid.UUID, addr string, pnl float64) *Wallet {
	w := &Wallet{ID: id, Chain: "evm", Address: addr, Dex: "hyperliquid"}
	m := &Metrics{RealizedPnl: &pnl}
	w.Metrics = m
	return w
}

// SCAN-H-01: no filters → 200, defaults timeframe 30D + sort pnl desc.
func TestHandler_Scan_NoFilters_Defaults(t *testing.T) {
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		return []*Wallet{sampleWallet(uuid.New(), "0xaaa", 150)}, 1, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastSort.Field != "pnl" || repo.lastSort.Order != "desc" {
		t.Errorf("default sort: got %+v", repo.lastSort)
	}
	if repo.lastFilters.Timeframe != Timeframe30D {
		t.Errorf("default timeframe: got %q", repo.lastFilters.Timeframe)
	}
	if repo.lastLimit != 50 || repo.lastOffset != 0 {
		t.Errorf("default pagination: limit=%d offset=%d", repo.lastLimit, repo.lastOffset)
	}
	resp := decode(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 wallet, got %d", len(data))
	}
}

// SCAN-H-19: meta carries the full total (not just pages).
func TestHandler_Scan_MetaContainsTotal(t *testing.T) {
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		return []*Wallet{sampleWallet(uuid.New(), "0xaaa", 1)}, 42, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?limit=10", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	meta := decode(t, w)["meta"].(map[string]interface{})
	if meta["total"] != float64(42) {
		t.Errorf("meta.total: expected 42, got %v", meta["total"])
	}
	if meta["total_pages"] != float64(5) {
		t.Errorf("meta.total_pages: got %v", meta["total_pages"])
	}
}

// SCAN-H-02: search passed to the repository (matched rows come from SQL).
func TestHandler_Scan_Search_PassedThrough(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?search=0xAbC", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Search != "0xAbC" {
		t.Errorf("search: got %q", repo.lastFilters.Search)
	}
}

// SCAN-H-03: single metric filter reaches the repository.
func TestHandler_Scan_PnlGt_PassedThrough(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?pnl_gt=100000", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(repo.lastFilters.Numeric) != 1 {
		t.Fatalf("expected 1 numeric filter, got %d", len(repo.lastFilters.Numeric))
	}
	nf := repo.lastFilters.Numeric[0]
	if nf.Metric != "pnl" || nf.Op != OpGT || nf.Lo != 100000 {
		t.Errorf("got %+v", nf)
	}
}

// SCAN-H-04: AND combination — both filters collected for SQL AND.
func TestHandler_Scan_AndCombination_BothCollected(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?pnl_gt=100000&win_rate_gt=60", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(repo.lastFilters.Numeric) != 2 {
		t.Fatalf("expected 2 numeric filters, got %d", len(repo.lastFilters.Numeric))
	}
}

// SCAN-H-05: OR multi-select — both dex values collected (BR-11).
func TestHandler_Scan_DexOr_BothCollected(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?dex=hyperliquid,gmx", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(repo.lastFilters.Dex) != 2 {
		t.Fatalf("expected 2 dex values, got %v", repo.lastFilters.Dex)
	}
}

// SCAN-H-06: NULL-metric wallet + numeric filter — the filter reaches SQL
// where NULL rows are excluded (BR-07); here we verify no wallet without
// metrics is injected when the filter is present (mock returns only
// matching rows, as the repository would).
func TestHandler_Scan_NumericFilter_MockReturnsOnlyMatches(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		return []*Wallet{sampleWallet(id, "0xmatch", 500)}, 1, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?pnl_gt=100", testUser)
	resp := decode(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected only matching wallet, got %d", len(data))
	}
	metrics := data[0].(map[string]interface{})["metrics"].(map[string]interface{})
	if metrics["realized_pnl"] != 500.0 {
		t.Errorf("got %v", metrics["realized_pnl"])
	}
}

// SCAN-H-07: sort=volume&order=asc parsed and passed through.
func TestHandler_Scan_SortVolumeAsc_PassedThrough(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?sort=volume&order=asc", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastSort.Field != "volume" || repo.lastSort.Order != "asc" {
		t.Errorf("got %+v", repo.lastSort)
	}
}

// SCAN-H-08: ties broken by (chain, address) happen in SQL; the handler
// must preserve the deterministic default (pnl desc) when sort is absent.
func TestHandler_Scan_TieBreakDefaultSortPreserved(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	if w := get(t, router, "/api/v1/wallets", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastSort.Field != "pnl" || repo.lastSort.Order != "desc" {
		t.Errorf("default sort not preserved: %+v", repo.lastSort)
	}
}

// SCAN-H-09: page=2&limit=10 → offset 10, limit 10.
func TestHandler_Scan_Pagination_OffsetLimit(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?page=2&limit=10", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastOffset != 10 || repo.lastLimit != 10 {
		t.Errorf("expected offset 10 limit 10, got offset=%d limit=%d", repo.lastOffset, repo.lastLimit)
	}
}

// SCAN-H-10: timeframe=24H vs ALL selects the right snapshot slot.
func TestHandler_Scan_TimeframeVariants_PassedThrough(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	if w := get(t, router, "/api/v1/wallets?timeframe=24H", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Timeframe != Timeframe24H {
		t.Errorf("got %q", repo.lastFilters.Timeframe)
	}

	if w := get(t, router, "/api/v1/wallets?timeframe=ALL", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Timeframe != TimeframeALL {
		t.Errorf("got %q", repo.lastFilters.Timeframe)
	}
}

// SCAN-H-11: invalid sort / operator / timeframe → 400 COMMON-902 (BE-05,
// BE-04, BE-03); the repository is never reached.
func TestHandler_Scan_InvalidParams_Returns400Common902(t *testing.T) {
	for name, target := range map[string]string{
		"bad sort":  "/api/v1/wallets?sort=sharpe",
		"bad order": "/api/v1/wallets?order=sideways",
		"bad op":    "/api/v1/wallets?pnl_approx=1",
		"bad value": "/api/v1/wallets?pnl_gt=abc",
		"bad tf":    "/api/v1/wallets?timeframe=1Y",
		"bad range": "/api/v1/wallets?pnl_between=10,5",
		"bad dex":   "/api/v1/wallets?dex=nope",
	} {
		repo := &mockRepo{}
		router := setupRouter(NewHandler(NewService(repo, testConfig())))
		w := get(t, router, target, testUser)
		expectCode(t, w, http.StatusBadRequest, string(domain.ErrCodeValidation))
		if repo.lastFilters != nil {
			t.Errorf("%s: repository must not be reached", name)
		}
	}
}

// SCAN-H-12: GET /wallets/:id existing → 200 + identity + metrics.
func TestHandler_Detail_Existing_Returns200(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		w := sampleWallet(wid, "0xabc", 42)
		return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	data := resp["data"].(map[string]interface{})
	if data["address"] != "0xabc" {
		t.Errorf("identity: got %v", data["address"])
	}
	if data["metrics"] == nil {
		t.Error("expected metrics in detail")
	}
}

// SCAN-H-13: unknown wallet → 404 WALLET-001 (BE-06).
func TestHandler_Detail_Unknown_Returns404(t *testing.T) {
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		return nil, pgx.ErrNoRows
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+uuid.New().String(), testUser)
	expectCode(t, w, http.StatusNotFound, string(domain.ErrCodeWalletNotFound))
}

// SCAN-H-14: wallet in user B's group → the detail response of user A
// never contains B's group membership (BE-06). The repository receives A's
// userID and returns only A's memberships; we assert the serialized JSON
// has no trace of B's group.
func TestHandler_Detail_NoForeignMembershipLeak(t *testing.T) {
	id := uuid.New()
	groupB := "Hedge Fund B"
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		if uid.String() != testUser {
			t.Errorf("expected caller %s, got %s", testUser, uid)
		}
		w := sampleWallet(wid, "0xabc", 1)
		// Simulate repository result for A: no memberships pointing to B.
		return &WalletDetail{Wallet: *w, Memberships: []GroupRef{
			{ID: uuid.New(), Name: "Group of A"},
		}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	body := w.Body.String()
	if len(body) > 0 && contains(body, groupB) {
		t.Errorf("foreign group leaked: %s", body)
	}
	memberships := raw["data"].(map[string]interface{})["memberships"].([]interface{})
	if len(memberships) != 1 {
		t.Errorf("expected only A's membership, got %d", len(memberships))
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// HasScannerParams drives the BE-09 routing decision.
func TestHasScannerParams(t *testing.T) {
	if HasScannerParams(url.Values{"search": {"x"}, "page": {"2"}, "limit": {"10"}}) {
		t.Error("Phase 1 params must not trigger the scanner")
	}
	if !HasScannerParams(url.Values{"timeframe": {"24H"}}) {
		t.Error("scanner param must trigger the scanner")
	}
	if !HasScannerParams(url.Values{"pnl_gt": {"1"}}) {
		t.Error("metric filter must trigger the scanner")
	}
}

var _ = api.Meta{} // keep api import if unused by later edits

func patchJSON(t *testing.T, router *gin.Engine, target, userID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, target, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", userID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TAG-H-01: PATCH valid tag → 200 with tag in the refreshed detail.
func TestHandler_UpdateTag_Valid_Returns200(t *testing.T) {
	id := uuid.New()
	var gotTag string
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
		upsertTagFn: func(ctx context.Context, uid, wid uuid.UUID, tag string) error {
			gotTag = tag
			return nil
		},
		detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
			w := sampleWallet(wid, "0xabc", 10)
			w.Tag = strPtr(gotTag)
			return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}}, nil
		},
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+id.String(), testUser,
		map[string]string{"tag": "  Binance hot  "})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotTag != "Binance hot" {
		t.Errorf("tag not trimmed on write: %q", gotTag)
	}
	data := decode(t, w)["data"].(map[string]interface{})
	if data["tag"] != "Binance hot" {
		t.Errorf("tag missing in response: %v", data["tag"])
	}
}

// TAG-H-02: PATCH unknown wallet → 404 WALLET-001.
func TestHandler_UpdateTag_Unknown_Returns404(t *testing.T) {
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return false, nil },
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+uuid.New().String(), testUser,
		map[string]string{"tag": "x"})
	expectCode(t, w, http.StatusNotFound, string(domain.ErrCodeWalletNotFound))
}

// TAG-H-03: too-long tag, bad ID, missing/wrong-typed field → 400 COMMON-902.
func TestHandler_UpdateTag_Invalid_Returns400(t *testing.T) {
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))
	id := uuid.New().String()

	long := ""
	for i := 0; i < MaxTagRunes+1; i++ {
		long += "a"
	}
	cases := map[string]interface{}{
		"too long":   map[string]string{"tag": long},
		"missing":    map[string]string{},
		"wrong type": map[string]interface{}{"tag": 5},
	}
	for name, body := range cases {
		w := patchJSON(t, router, "/api/v1/wallets/"+id, testUser, body)
		expectCode(t, w, http.StatusBadRequest, string(domain.ErrCodeValidation))
		_ = name
	}

	w := patchJSON(t, router, "/api/v1/wallets/not-a-uuid", testUser,
		map[string]string{"tag": "x"})
	expectCode(t, w, http.StatusBadRequest, string(domain.ErrCodeValidation))
}

// TAG-H-04: PATCH empty string clears the tag (ClearTag, detail tag null).
func TestHandler_UpdateTag_Empty_ClearsTag(t *testing.T) {
	id := uuid.New()
	cleared := false
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
		clearTagFn: func(ctx context.Context, uid, wid uuid.UUID) error {
			cleared = true
			return nil
		},
		detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
			w := sampleWallet(wid, "0xabc", 10)
			return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}}, nil
		},
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+id.String(), testUser,
		map[string]string{"tag": "   "})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !cleared {
		t.Error("expected ClearTag to be called")
	}
	if data := decode(t, w)["data"].(map[string]interface{}); data["tag"] != nil {
		t.Errorf("expected null tag, got %v", data["tag"])
	}
}

// TAG-H-05: GET detail carries the caller's tag.
func TestHandler_Detail_IncludesTag(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		w := sampleWallet(wid, "0xabc", 42)
		w.Tag = strPtr("Mine")
		return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if data := decode(t, w)["data"].(map[string]interface{}); data["tag"] != "Mine" {
		t.Errorf("tag missing in detail: %v", data["tag"])
	}
}

// TAG-H-06: scan rows carry tags; search text reaches the repository
// (tag matching itself is SQL — covered by TAG-I-01).
func TestHandler_Scan_RowsCarryTag(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		w := sampleWallet(id, "0xmatch", 500)
		w.Tag = strPtr("tagged")
		return []*Wallet{w}, 1, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?search=tagged", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Search != "tagged" {
		t.Errorf("search not forwarded: %q", repo.lastFilters.Search)
	}
	rows := decode(t, w)["data"].([]interface{})
	if len(rows) != 1 || rows[0].(map[string]interface{})["tag"] != "tagged" {
		t.Errorf("tag missing in scan row: %v", rows)
	}
}

// CFG-H-01: GET /wallets/filter-config → 200 with the full config payload.
func TestHandler_FilterConfig_Returns200(t *testing.T) {
	router := setupRouter(NewHandler(NewService(&mockRepo{}, testConfig())))

	w := get(t, router, "/api/v1/wallets/filter-config", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decode(t, w)["data"].(map[string]interface{})
	for _, key := range []string{"dexes", "chains", "markets", "timeframes",
		"sort_fields", "metrics", "operators"} {
		if _, ok := data[key]; !ok {
			t.Errorf("config missing key %q", key)
		}
	}
}

// CFG-H-02: filter-config is public — no auth needed (static enums only).
func TestHandler_FilterConfig_Anonymous_Returns200(t *testing.T) {
	router := setupRouter(NewHandler(NewService(&mockRepo{}, testConfig())))

	w := get(t, router, "/api/v1/wallets/filter-config", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for anonymous config, got %d", w.Code)
	}
}

// CFG-H-03: metrics[] carries min/max/ops for every filterable metric.
func TestHandler_FilterConfig_MetricsContent(t *testing.T) {
	router := setupRouter(NewHandler(NewService(&mockRepo{}, testConfig())))

	w := get(t, router, "/api/v1/wallets/filter-config", testUser)
	metrics := decode(t, w)["data"].(map[string]interface{})["metrics"].([]interface{})
	if len(metrics) != len(numericMetrics) {
		t.Fatalf("expected %d metrics, got %d", len(numericMetrics), len(metrics))
	}
	found := map[string]bool{}
	for _, raw := range metrics {
		m := raw.(map[string]interface{})
		found[m["key"].(string)] = true
		if _, ok := m["ops"]; !ok {
			t.Errorf("metric %v missing ops", m["key"])
		}
		if _, ok := m["sortable"]; !ok {
			t.Errorf("metric %v missing sortable", m["key"])
		}
	}
	for key := range numericMetrics {
		if !found[key] {
			t.Errorf("metrics missing %q", key)
		}
	}
}

// CFG-H-04: timeframe/sort enums and defaults match the parser tables.
func TestHandler_FilterConfig_EnumsMatchParser(t *testing.T) {
	router := setupRouter(NewHandler(NewService(&mockRepo{}, testConfig())))

	w := get(t, router, "/api/v1/wallets/filter-config", testUser)
	data := decode(t, w)["data"].(map[string]interface{})

	if data["default_timeframe"] != Timeframe30D {
		t.Errorf("default_timeframe: got %v", data["default_timeframe"])
	}
	if data["default_sort"] != "pnl" {
		t.Errorf("default_sort: got %v", data["default_sort"])
	}
	tfs := data["timeframes"].([]interface{})
	if len(tfs) != len(validTimeframes) {
		t.Errorf("timeframes: got %v", tfs)
	}
	sfs := data["sort_fields"].([]interface{})
	if len(sfs) != len(sortableFields) {
		t.Errorf("sort_fields: got %v", sfs)
	}
}

// WL-H-01/WL-H-02: PATCH {watchlisted:true|false} stars/unstars and the
// refreshed detail reflects it.
func TestHandler_Patch_Watchlisted_Toggles(t *testing.T) {
	id := uuid.New()
	starred := false
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
		watchFn: func(ctx context.Context, uid, wid uuid.UUID, on bool) error {
			starred = on
			return nil
		},
		detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
			w := sampleWallet(wid, "0xabc", 10)
			w.Watchlisted = starred
			return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}, Positions: []Position{}}, nil
		},
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))
	target := "/api/v1/wallets/" + id.String()

	w := patchJSON(t, router, target, testUser, map[string]bool{"watchlisted": true})
	if w.Code != http.StatusOK {
		t.Fatalf("star: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !starred {
		t.Error("expected SetWatchlisted(true)")
	}
	if data := decode(t, w)["data"].(map[string]interface{}); data["watchlisted"] != true {
		t.Errorf("detail watchlisted: got %v", data["watchlisted"])
	}

	w = patchJSON(t, router, target, testUser, map[string]bool{"watchlisted": false})
	if w.Code != http.StatusOK {
		t.Fatalf("unstar: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if starred {
		t.Error("expected SetWatchlisted(false)")
	}
	if data := decode(t, w)["data"].(map[string]interface{}); data["watchlisted"] != false {
		t.Errorf("detail watchlisted: got %v", data["watchlisted"])
	}
}

// WL-H-03: empty body (neither tag nor watchlisted) → 400 COMMON-902.
func TestHandler_Patch_EmptyBody_Returns400(t *testing.T) {
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+uuid.New().String(), testUser, map[string]string{})
	expectCode(t, w, http.StatusBadRequest, string(domain.ErrCodeValidation))
}

// WL-H-04: tag and watchlisted in one PATCH both apply.
func TestHandler_Patch_BothFields_Applied(t *testing.T) {
	id := uuid.New()
	var gotTag string
	var gotStar bool
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return true, nil },
		upsertTagFn: func(ctx context.Context, uid, wid uuid.UUID, tag string) error {
			gotTag = tag
			return nil
		},
		watchFn: func(ctx context.Context, uid, wid uuid.UUID, on bool) error {
			gotStar = on
			return nil
		},
		detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
			w := sampleWallet(wid, "0xabc", 10)
			w.Tag = strPtr(gotTag)
			w.Watchlisted = gotStar
			return &WalletDetail{Wallet: *w, Memberships: []GroupRef{}, Positions: []Position{}}, nil
		},
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+id.String(), testUser,
		map[string]interface{}{"tag": "combo", "watchlisted": true})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotTag != "combo" || !gotStar {
		t.Errorf("both fields must apply: tag=%q star=%v", gotTag, gotStar)
	}
	data := decode(t, w)["data"].(map[string]interface{})
	if data["tag"] != "combo" || data["watchlisted"] != true {
		t.Errorf("response: %v", data)
	}
}

// WL-H-05: PATCH unknown wallet → 404 WALLET-001.
func TestHandler_Patch_Watchlisted_UnknownWallet404(t *testing.T) {
	repo := &mockRepo{
		existsFn: func(ctx context.Context, wid uuid.UUID) (bool, error) { return false, nil },
	}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+uuid.New().String(), testUser,
		map[string]bool{"watchlisted": true})
	expectCode(t, w, http.StatusNotFound, string(domain.ErrCodeWalletNotFound))
}

// WL-H-06: ?watchlisted=true reaches the repository as a set flag; absent
// stays nil (no filter).
func TestHandler_Scan_WatchlistedParam_PassedThrough(t *testing.T) {
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		return []*Wallet{}, 0, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	if w := get(t, router, "/api/v1/wallets?watchlisted=true", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Watchlisted == nil || !*repo.lastFilters.Watchlisted {
		t.Errorf("watchlisted=true not forwarded: %v", repo.lastFilters.Watchlisted)
	}

	if w := get(t, router, "/api/v1/wallets", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilters.Watchlisted != nil {
		t.Errorf("absent must stay nil, got %v", *repo.lastFilters.Watchlisted)
	}
}

// WL-H-07: scan rows carry the caller's star state.
func TestHandler_Scan_RowsCarryWatchlisted(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		w := sampleWallet(id, "0xstarred", 500)
		w.Watchlisted = true
		return []*Wallet{w}, 1, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets", testUser)
	rows := decode(t, w)["data"].([]interface{})
	if len(rows) != 1 || rows[0].(map[string]interface{})["watchlisted"] != true {
		t.Errorf("watchlisted missing in scan row: %v", rows)
	}
}

// POS-H-01: detail carries the per-market positions breakdown.
func TestHandler_Detail_PositionsIncluded(t *testing.T) {
	id := uuid.New()
	pnl := 21000.0
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		w := sampleWallet(wid, "0xabc", 100)
		return &WalletDetail{
			Wallet:      *w,
			Memberships: []GroupRef{},
			Positions: []Position{
				{Market: "BTC", Metrics: Metrics{RealizedPnl: &pnl}},
			},
		}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	data := decode(t, w)["data"].(map[string]interface{})
	positions, ok := data["positions"].([]interface{})
	if !ok || len(positions) != 1 {
		t.Fatalf("positions missing: %v", data)
	}
	pos := positions[0].(map[string]interface{})
	if pos["market"] != "BTC" || pos["realized_pnl"] != pnl {
		t.Errorf("position row: %v", pos)
	}
}

// POS-H-02: no per-market snapshots → positions is [] (never null).
func TestHandler_Detail_PositionsEmpty_NotNull(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		return &WalletDetail{Wallet: *sampleWallet(wid, "0xabc", 1), Memberships: []GroupRef{}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), testUser)
	data := decode(t, w)["data"].(map[string]interface{})
	positions, ok := data["positions"].([]interface{})
	if !ok {
		t.Fatalf("positions must be an array, got %v", data["positions"])
	}
	if len(positions) != 0 {
		t.Errorf("expected empty positions, got %v", positions)
	}
}

// POS-H-03: timeframe flows into the positions query through GetDetail.
func TestHandler_Detail_TimeframeAppliedToPositions(t *testing.T) {
	id := uuid.UUID{}
	var gotTF string
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		gotTF = snapshotKey(f)
		return &WalletDetail{Wallet: *sampleWallet(wid, "0xabc", 1),
			Memberships: []GroupRef{}, Positions: []Position{}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	if w := get(t, router, "/api/v1/wallets/"+id.String()+"?timeframe=24H", testUser); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotTF != Timeframe24H {
		t.Errorf("positions timeframe: got %q", gotTF)
	}
}

// PUB-H-01: GET /wallets without a token ? 200, repo receives uuid.Nil
// (anonymous) and rows carry no personal data.
func TestHandler_Scan_Anonymous_Returns200(t *testing.T) {
	id := uuid.New()
	var gotUser uuid.UUID
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		gotUser = u
		w := sampleWallet(id, "0xanon", 100)
		return []*Wallet{w}, 1, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous scan: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUser != uuid.Nil {
		t.Errorf("anonymous scan must pass uuid.Nil, got %v", gotUser)
	}
	row := decode(t, w)["data"].([]interface{})[0].(map[string]interface{})
	if row["tag"] != nil || row["watchlisted"] != false {
		t.Errorf("anonymous row must have empty personal fields: %v", row)
	}
}

// PUB-H-02: ?watchlisted=true without a token ? 401 AUTH-003 (the star
// filter is meaningless without a caller).
func TestHandler_Scan_Anonymous_WatchlistedFilter_401(t *testing.T) {
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		return []*Wallet{}, 0, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets?watchlisted=true", "")
	expectCode(t, w, http.StatusUnauthorized, "AUTH-003")
	if repo.lastFilters != nil {
		t.Error("repository must not be reached for anonymous watchlist filter")
	}
}

// PUB-H-03: authenticated scan still personalizes (sanity that optional
// auth did not disable the user path).
func TestHandler_Scan_Authenticated_Personalizes(t *testing.T) {
	id := uuid.New()
	var gotUser uuid.UUID
	repo := &mockRepo{scanFn: func(ctx context.Context, f *Filters, s *SortSpec, g *uuid.UUID, u uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
		gotUser = u
		return []*Wallet{}, 0, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets", testUser)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotUser.String() != testUser {
		t.Errorf("authenticated scan must pass the caller, got %v", gotUser)
	}
	_ = id
}

// PUB-H-04: GET /wallets/:id without a token ? 200 (public drawer).
func TestHandler_Detail_Anonymous_Returns200(t *testing.T) {
	id := uuid.New()
	var gotUser uuid.UUID
	repo := &mockRepo{detailFn: func(ctx context.Context, wid, uid uuid.UUID, f *Filters) (*WalletDetail, error) {
		gotUser = uid
		return &WalletDetail{Wallet: *sampleWallet(wid, "0xanon", 5),
			Memberships: []GroupRef{}, Positions: []Position{}}, nil
	}}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := get(t, router, "/api/v1/wallets/"+id.String(), "")
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous detail: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUser != uuid.Nil {
		t.Errorf("anonymous detail must pass uuid.Nil, got %v", gotUser)
	}
}

// PUB-H-05: PATCH without a token ? 403 AUTH-005 (writes stay auth-only).
func TestHandler_Patch_Anonymous_Rejected(t *testing.T) {
	repo := &mockRepo{}
	router := setupRouter(NewHandler(NewService(repo, testConfig())))

	w := patchJSON(t, router, "/api/v1/wallets/"+uuid.New().String(), "",
		map[string]bool{"watchlisted": true})
	if w.Code != http.StatusForbidden {
		t.Errorf("anonymous PATCH: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
