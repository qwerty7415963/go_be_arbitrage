package fundingarbitrage

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ListPerpVenues godoc
// @Summary      List perp venues
// @Description  Get all active venues that support perpetual trading
// @Tags         funding-arbitrage
// @Produce      json
// @Param        market  query     string  false  "Filter by market type"  Enums(perp)
// @Success      200     {object}  api.Response{data=[]VenuePerp}
// @Router       /api/v1/public/venues [get]
func (h *Handler) ListPerpVenues(c *gin.Context) {
	venues, err := h.service.GetPerpVenues(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    venues,
	})
}

// GetFundingArbitrage godoc
// @Summary      Get funding arbitrage opportunities
// @Description  Get funding arbitrage table for selected venues
// @Tags         funding-arbitrage
// @Produce      json
// @Param        venue_id      query     []string  true   "Venue IDs (min 2, max 10)"
// @Param        sort          query     string    false  "Sort by"  Enums(rate_1h_desc, rate_8h_desc, apr_desc, spread_desc)  Default(rate_8h_desc)
// @Param        page          query     int       false  "Page number"  Default(1)  Minimum(1)
// @Param        limit         query     int       false  "Items per page"  Default(50)  Minimum(1)  Maximum(200)
// @Param        offset        query     int       false  "Offset (alternative to page)"  Default(0)  Minimum(0)
// @Param        include_stale query     bool      false  "Include stale data"  Default(false)
// @Param        refresh       query     bool      false  "Force refresh cache"  Default(false)
// @Success      200           {object}  api.Response{data=FundingArbitrageResponse,meta=api.Meta}
// @Failure      400           {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/funding/arbitrage [get]
func (h *Handler) GetFundingArbitrage(c *gin.Context) {
	// Parse venue_ids (handles both duplicate params and comma-separated)
	venueIDStrs := c.QueryArray("venue_id")
	if len(venueIDStrs) == 0 {
		if venuesStr := c.Query("venue_id"); venuesStr != "" {
			venueIDStrs = strings.Split(venuesStr, ",")
		}
	}

	// Expand any comma-separated values within each element
	var expanded []string
	for _, s := range venueIDStrs {
		for _, part := range strings.Split(s, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				expanded = append(expanded, trimmed)
			}
		}
	}
	venueIDStrs = expanded

	if len(venueIDStrs) < 2 {
		respondValidationError(c, "at least 2 venue_ids required")
		return
	}

	if len(venueIDStrs) > 10 {
		respondValidationError(c, "maximum 10 venue_ids allowed")
		return
	}

	// Parse and validate venue IDs
	var venueIDs []uuid.UUID
	seen := make(map[uuid.UUID]bool)
	for _, idStr := range venueIDStrs {
		id, err := uuid.Parse(strings.TrimSpace(idStr))
		if err != nil {
			respondValidationError(c, "invalid venue_id: "+idStr)
			return
		}
		if seen[id] {
			respondValidationError(c, "duplicate venue_id: "+idStr)
			return
		}
		seen[id] = true
		venueIDs = append(venueIDs, id)
	}

	// Parse sort
	sortBy := c.DefaultQuery("sort", "rate_8h_desc")
	validSorts := map[string]bool{
		"rate_1h_desc": true,
		"rate_8h_desc": true,
		"apr_desc":     true,
		"spread_desc":  true,
	}
	if !validSorts[sortBy] {
		respondValidationError(c, "invalid sort value")
		return
	}

	// Parse limit
	limit := defaultLimit
	if limitStr := c.Query("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l < 1 || l > maxLimit {
			respondValidationError(c, "limit must be between 1 and 200")
			return
		}
		limit = l
	}

	// Parse offset and page (page takes priority)
	offset := 0
	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			respondValidationError(c, "page must be >= 1")
			return
		}
		page = p
		offset = (page - 1) * limit
	} else if offsetStr := c.Query("offset"); offsetStr != "" {
		o, err := strconv.Atoi(offsetStr)
		if err != nil || o < 0 {
			respondValidationError(c, "offset must be >= 0")
			return
		}
		offset = o
		page = offset/limit + 1
	}

	// Parse include_stale
	includeStale := c.Query("include_stale") == "true"

	// Parse refresh
	forceRefresh := c.Query("refresh") == "true"

	// Get funding arbitrage
	result, err := h.service.GetFundingArbitrage(
		c.Request.Context(),
		venueIDs,
		sortBy,
		includeStale,
		forceRefresh,
	)
	if err != nil {
		respondError(c, err)
		return
	}

	// Global flatten → sort → paginate → redistribute
	flattened := flattenTokens(result.Pairs, sortBy)
	sortFlattenedByParam(flattened, sortBy)
	totalUnique := len(flattened)

	paginated, hasMore := applyGlobalPagination(flattened, offset, limit)
	result.Pairs = redistributeTokens(result.Pairs, paginated)

	// Compute total_pages
	totalPages := computeTotalPages(totalUnique, limit)

	// Build response with meta
	meta := &api.Meta{
		Page:       page,
		TotalPages: totalPages,
		Limit:      limit,
		Offset:     offset,
		HasMore:    hasMore,
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    result,
		Meta:    meta,
	})
}

func respondValidationError(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, api.Response{
		Success: false,
		Error: &api.ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: message,
		},
	})
}

func respondError(c *gin.Context, err error) {
	appErr, ok := err.(*domain.AppError)
	if !ok {
		appErr = domain.WrapError(domain.ErrCodeInternal, err.Error(), err)
	}

	c.JSON(appErr.StatusCode, api.Response{
		Success: false,
		Error: &api.ErrorBody{
			Code:    string(appErr.Code),
			Message: appErr.Message,
			Details: appErr.Details,
		},
	})
}

// ─── Global pagination helpers ────────────────────────────────

func computeTotalPages(total, limit int) int {
	if limit <= 0 || total <= 0 {
		return 0
	}
	return (total + limit - 1) / limit
}

// getSortValue returns the sort field value for a token based on sortBy param.
func getSortValue(t *ArbitrageToken, sortBy string) float64 {
	var ptr *float64
	switch sortBy {
	case "rate_1h_desc":
		ptr = t.Rate1hPercent
	case "rate_8h_desc":
		ptr = t.Rate8hPercent
	case "apr_desc":
		ptr = t.APRPercent
	case "spread_desc":
		ptr = t.PriceSpreadPercent
	default:
		ptr = t.Rate8hPercent
	}
	if ptr == nil {
		return -1
	}
	return *ptr
}

// isBetter returns true if token a has a higher sort value than b.
func isBetter(a, b *ArbitrageToken, sortBy string) bool {
	return getSortValue(a, sortBy) > getSortValue(b, sortBy)
}

// flattenTokens deduplicates tokens across all pairs by symbol, keeping the token with the highest sort value.
func flattenTokens(pairs []Pair, sortBy string) []ArbitrageToken {
	best := make(map[string]*ArbitrageToken)
	for _, p := range pairs {
		for i := range p.Tokens {
			t := &p.Tokens[i]
			existing, ok := best[t.Symbol]
			if !ok || isBetter(t, existing, sortBy) {
				best[t.Symbol] = t
			}
		}
	}
	result := make([]ArbitrageToken, 0, len(best))
	for _, t := range best {
		result = append(result, *t)
	}
	return result
}

// sortFlattenedByParam sorts the flattened token list by the sort param (descending).
func sortFlattenedByParam(tokens []ArbitrageToken, sortBy string) {
	SortTokens(tokens, sortBy)
}

// applyGlobalPagination slices the flattened token list and returns hasMore.
func applyGlobalPagination(flattened []ArbitrageToken, offset, limit int) ([]ArbitrageToken, bool) {
	if flattened == nil {
		flattened = []ArbitrageToken{}
	}
	total := len(flattened)
	if offset >= total {
		return []ArbitrageToken{}, false
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return flattened[offset:end], end < total
}

// redistributeTokens filters each pair to keep only tokens present in the paginated list.
func redistributeTokens(pairs []Pair, paginated []ArbitrageToken) []Pair {
	allowed := make(map[string]bool, len(paginated))
	for _, t := range paginated {
		allowed[t.Symbol] = true
	}

	result := make([]Pair, len(pairs))
	for i, p := range pairs {
		result[i] = p
		result[i].Tokens = nil
		for _, t := range p.Tokens {
			if allowed[t.Symbol] {
				result[i].Tokens = append(result[i].Tokens, t)
			}
		}
	}
	return result
}
