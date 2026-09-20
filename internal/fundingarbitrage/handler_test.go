package fundingarbitrage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		v1.GET("/public/venues", handler.ListPerpVenues)
		v1.GET("/funding/arbitrage", handler.GetFundingArbitrage)
	}
	return router
}

func makeTokens(n int) []ArbitrageToken {
	tokens := make([]ArbitrageToken, n)
	for i := range tokens {
		tokens[i] = ArbitrageToken{
			InstrumentID: uuid.New(),
			Symbol:       "TOKEN-" + uuid.New().String()[:8],
		}
	}
	return tokens
}

func TestHandler_GetFundingArbitrage_MissingVenues(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/funding/arbitrage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_GetFundingArbitrage_OnlyOneVenue(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/funding/arbitrage?venue_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetFundingArbitrage_InvalidSort(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	ids := uuid.New().String() + "&venue_id=" + uuid.New().String()
	req, _ := http.NewRequest("GET", "/api/v1/funding/arbitrage?venue_id="+ids+"&sort=invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetFundingArbitrage_InvalidLimit(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	ids := uuid.New().String() + "&venue_id=" + uuid.New().String()
	req, _ := http.NewRequest("GET", "/api/v1/funding/arbitrage?venue_id="+ids+"&limit=999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_GetFundingArbitrage_InvalidOffset(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	ids := uuid.New().String() + "&venue_id=" + uuid.New().String()
	req, _ := http.NewRequest("GET", "/api/v1/funding/arbitrage?venue_id="+ids+"&offset=-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── Pagination logic unit tests ─────────────────────────────

func TestPagination_Offset0_Limit10(t *testing.T) {
	tokens := makeTokens(25)
	result := applyPagination(tokens, 0, 10)

	if len(result) != 10 {
		t.Fatalf("expected 10 tokens, got %d", len(result))
	}
}

func TestPagination_Offset10_Limit10(t *testing.T) {
	tokens := makeTokens(25)
	result := applyPagination(tokens, 10, 10)

	if len(result) != 10 {
		t.Fatalf("expected 10 tokens, got %d", len(result))
	}
	// Should be different tokens than page 1
	page1 := applyPagination(tokens, 0, 10)
	if result[0].InstrumentID == page1[0].InstrumentID {
		t.Error("page 2 should start after page 1")
	}
}

func TestPagination_Offset20_Limit10_PartialPage(t *testing.T) {
	tokens := makeTokens(25)
	result := applyPagination(tokens, 20, 10)

	if len(result) != 5 {
		t.Fatalf("expected 5 tokens (partial page), got %d", len(result))
	}
}

func TestPagination_OffsetExceedsTotal_EmptyResult(t *testing.T) {
	tokens := makeTokens(10)
	result := applyPagination(tokens, 50, 10)

	if len(result) != 0 {
		t.Fatalf("expected 0 tokens, got %d", len(result))
	}
}

func TestPagination_EmptyTokens(t *testing.T) {
	result := applyPagination(nil, 0, 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 tokens, got %d", len(result))
	}
}

func TestPagination_HasMore(t *testing.T) {
	tests := []struct {
		total   int
		offset  int
		limit   int
		wantHas bool
	}{
		{25, 0, 10, true},
		{25, 20, 10, false},
		{10, 0, 10, false},
		{10, 0, 5, true},
		{0, 0, 10, false},
	}

	for _, tt := range tests {
		hasMore := computeHasMore(tt.total, tt.offset, tt.limit)
		if hasMore != tt.wantHas {
			t.Errorf("total=%d offset=%d limit=%d: expected hasMore=%v, got %v",
				tt.total, tt.offset, tt.limit, tt.wantHas, hasMore)
		}
	}
}

func TestPagination_PageAndTotalPages(t *testing.T) {
	tests := []struct {
		total       int
		offset      int
		limit       int
		wantPage    int
		wantTotal   int
	}{
		{25, 0, 10, 1, 3},
		{25, 10, 10, 2, 3},
		{25, 20, 10, 3, 3},
		{10, 0, 10, 1, 1},
		{10, 0, 5, 1, 2},
		{10, 5, 5, 2, 2},
		{0, 0, 10, 1, 0},
		{1, 0, 50, 1, 1},
		{100, 0, 20, 1, 5},
	}

	for _, tt := range tests {
		page := computePage(tt.offset, tt.limit)
		totalPages := computeTotalPages(tt.total, tt.limit)
		if page != tt.wantPage {
			t.Errorf("total=%d offset=%d limit=%d: expected page=%d, got %d",
				tt.total, tt.offset, tt.limit, tt.wantPage, page)
		}
		if totalPages != tt.wantTotal {
			t.Errorf("total=%d offset=%d limit=%d: expected totalPages=%d, got %d",
				tt.total, tt.offset, tt.limit, tt.wantTotal, totalPages)
		}
	}
}

func TestPagination_MetaResponse(t *testing.T) {
	pairs := []Pair{
		{
			VenueA: VenueInfo{ID: uuid.New(), Code: "A"},
			VenueB: VenueInfo{ID: uuid.New(), Code: "B"},
			Tokens: makeTokens(25),
		},
	}

	offset, limit := 0, 10
	flattened := flattenTokens(pairs)
	paginated, hasMore := applyGlobalPagination(flattened, offset, limit)
	result := redistributeTokens(pairs, paginated)

	totalTokens := 0
	for _, p := range result {
		totalTokens += len(p.Tokens)
	}
	if totalTokens != 10 {
		t.Fatalf("expected 10 tokens total, got %d", totalTokens)
	}
	if !hasMore {
		t.Error("expected hasMore=true")
	}

	resp := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"pairs": result,
		},
		"meta": map[string]interface{}{
			"offset":   offset,
			"limit":    limit,
			"has_more": hasMore,
		},
	}
	data, _ := json.Marshal(resp)
	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}

// ─── Global pagination unit tests ─────────────────────────────

func makeTokenWithSymbol(symbol string) ArbitrageToken {
	return ArbitrageToken{
		InstrumentID: uuid.New(),
		Symbol:       symbol,
	}
}

func TestFlattenTokens_EmptyPairs(t *testing.T) {
	result := flattenTokens(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

func TestFlattenTokens_NoOverlap(t *testing.T) {
	pairs := []Pair{
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH")}},
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("SOL"), makeTokenWithSymbol("XRP")}},
	}
	result := flattenTokens(pairs)
	if len(result) != 4 {
		t.Fatalf("expected 4, got %d", len(result))
	}
}

func TestFlattenTokens_WithOverlap(t *testing.T) {
	pairs := []Pair{
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH")}},
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("SOL")}},
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("DOGE")}},
	}
	result := flattenTokens(pairs)
	if len(result) != 4 {
		t.Fatalf("expected 4 unique, got %d", len(result))
	}
	symbols := map[string]bool{}
	for _, t := range result {
		symbols[t.Symbol] = true
	}
	for _, s := range []string{"BTC", "ETH", "SOL", "DOGE"} {
		if !symbols[s] {
			t.Errorf("missing symbol %s", s)
		}
	}
}

func TestFlattenTokens_NilTokens(t *testing.T) {
	pairs := []Pair{
		{Tokens: nil},
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC")}},
	}
	result := flattenTokens(pairs)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestApplyGlobalPagination_Basic(t *testing.T) {
	tokens := makeTokens(25)
	result, hasMore := applyGlobalPagination(tokens, 0, 10)
	if len(result) != 10 {
		t.Fatalf("expected 10, got %d", len(result))
	}
	if !hasMore {
		t.Error("expected hasMore=true")
	}
}

func TestApplyGlobalPagination_PartialPage(t *testing.T) {
	tokens := makeTokens(7)
	result, hasMore := applyGlobalPagination(tokens, 0, 10)
	if len(result) != 7 {
		t.Fatalf("expected 7, got %d", len(result))
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
}

func TestApplyGlobalPagination_OffsetExceedsTotal(t *testing.T) {
	tokens := makeTokens(5)
	result, hasMore := applyGlobalPagination(tokens, 50, 10)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
}

func TestApplyGlobalPagination_EmptyTokens(t *testing.T) {
	result, hasMore := applyGlobalPagination(nil, 0, 10)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
}

func TestRedistributeTokens_Basic(t *testing.T) {
	pairs := []Pair{
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH"), makeTokenWithSymbol("SOL")}},
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("XRP")}},
	}
	paginated := []ArbitrageToken{makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH")}
	result := redistributeTokens(pairs, paginated)

	if len(result[0].Tokens) != 2 {
		t.Errorf("pair 0: expected 2, got %d", len(result[0].Tokens))
	}
	if len(result[1].Tokens) != 1 {
		t.Errorf("pair 1: expected 1, got %d", len(result[1].Tokens))
	}
}

func TestRedistributeTokens_EmptyPaginated(t *testing.T) {
	pairs := []Pair{
		{Tokens: []ArbitrageToken{makeTokenWithSymbol("BTC")}},
	}
	result := redistributeTokens(pairs, []ArbitrageToken{})
	if len(result[0].Tokens) != 0 {
		t.Fatalf("expected 0, got %d", len(result[0].Tokens))
	}
}

func TestGlobalPagination_DistributedCorrectly(t *testing.T) {
	// 3 pairs with overlapping tokens
	pairs := []Pair{
		{Tokens: []ArbitrageToken{
			makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH"),
			makeTokenWithSymbol("SOL"), makeTokenWithSymbol("XRP"),
		}},
		{Tokens: []ArbitrageToken{
			makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ADA"),
			makeTokenWithSymbol("DOGE"), makeTokenWithSymbol("AVAX"),
		}},
		{Tokens: []ArbitrageToken{
			makeTokenWithSymbol("BTC"), makeTokenWithSymbol("ETH"),
			makeTokenWithSymbol("DOT"), makeTokenWithSymbol("LINK"),
		}},
	}

	flattened := flattenTokens(pairs)
	if len(flattened) != 9 {
		t.Fatalf("expected 9 unique tokens, got %d", len(flattened))
	}

	paginated, _ := applyGlobalPagination(flattened, 0, 5)
	if len(paginated) != 5 {
		t.Fatalf("expected 5 paginated, got %d", len(paginated))
	}

	result := redistributeTokens(pairs, paginated)

	// No pair should have tokens not in paginated list
	allowed := map[string]bool{}
	for _, tok := range paginated {
		allowed[tok.Symbol] = true
	}
	for i, p := range result {
		for _, tok := range p.Tokens {
			if !allowed[tok.Symbol] {
				t.Errorf("pair %d has token %s not in paginated list", i, tok.Symbol)
			}
		}
	}

	// Each paginated token should appear in at least one pair
	present := map[string]bool{}
	for _, p := range result {
		for _, tok := range p.Tokens {
			present[tok.Symbol] = true
		}
	}
	for _, tok := range paginated {
		if !present[tok.Symbol] {
			t.Errorf("paginated token %s not found in any pair", tok.Symbol)
		}
	}
}

func TestGlobalPagination_MetaCalculation(t *testing.T) {
	tests := []struct {
		total       int
		limit       int
		wantTotal   int
		wantHasMore bool
	}{
		{25, 10, 3, true},
		{7, 10, 1, false},
		{10, 10, 1, false},
		{20, 5, 4, false},
		{0, 10, 0, false},
	}

	for _, tt := range tests {
		totalPages := computeTotalPages(tt.total, tt.limit)
		if totalPages != tt.wantTotal {
			t.Errorf("total=%d limit=%d: expected totalPages=%d, got %d",
				tt.total, tt.limit, tt.wantTotal, totalPages)
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func applyPagination(tokens []ArbitrageToken, offset, limit int) []ArbitrageToken {
	if tokens == nil {
		tokens = []ArbitrageToken{}
	}
	total := len(tokens)
	if offset >= total {
		return []ArbitrageToken{}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return tokens[offset:end]
}

func computeHasMore(total, offset, limit int) bool {
	return offset+limit < total
}

func computePage(offset, limit int) int {
	if limit <= 0 {
		return 1
	}
	return offset/limit + 1
}
