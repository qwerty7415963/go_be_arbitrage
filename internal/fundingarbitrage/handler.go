package fundingarbitrage

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
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
// @Param        sort          query     string    false  "Sort by"  Enums(apr_1h_desc, apr_4h_desc, apy_desc, spread_desc)  Default(apr_4h_desc)
// @Param        include_stale query     bool      false  "Include stale data"  Default(false)
// @Param        refresh       query     bool      false  "Force refresh cache"  Default(false)
// @Success      200           {object}  api.Response{data=FundingArbitrageResponse}
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
	sortBy := c.DefaultQuery("sort", "apr_4h_desc")
	validSorts := map[string]bool{
		"apr_1h_desc": true,
		"apr_4h_desc": true,
		"apy_desc":    true,
		"spread_desc": true,
	}
	if !validSorts[sortBy] {
		respondValidationError(c, "invalid sort value")
		return
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

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    result,
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
