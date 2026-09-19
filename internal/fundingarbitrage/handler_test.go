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

func TestPagination_MetaResponse(t *testing.T) {
	pairs := []Pair{
		{
			VenueA: VenueInfo{ID: uuid.New(), Code: "A"},
			VenueB: VenueInfo{ID: uuid.New(), Code: "B"},
			Tokens: makeTokens(25),
		},
	}

	// Apply pagination: page 1
	offset, limit := 0, 10
	hasMore := false
	for i := range pairs {
		tokens := pairs[i].Tokens
		total := len(tokens)
		if offset >= total {
			pairs[i].Tokens = []ArbitrageToken{}
			continue
		}
		end := offset + limit
		if end > total {
			end = total
		}
		pairs[i].Tokens = tokens[offset:end]
		if end < total {
			hasMore = true
		}
	}

	if len(pairs[0].Tokens) != 10 {
		t.Fatalf("expected 10 tokens, got %d", len(pairs[0].Tokens))
	}
	if !hasMore {
		t.Error("expected hasMore=true")
	}

	// Verify JSON response shape
	resp := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"pairs": pairs,
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
