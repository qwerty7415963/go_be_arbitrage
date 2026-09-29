package wallet

import (
	"errors"
	"net/http"
	"net/url"
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

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	wallets := router.Group("/wallets")
	if len(jwtMiddleware) > 0 {
		wallets.Use(jwtMiddleware[0])
	}
	{
		wallets.GET("", h.Scan)
		wallets.GET("/filter-config", h.FilterConfig)
		wallets.GET("/:id", h.Detail)
		wallets.PATCH("/:id", h.UpdateTag)
	}
}

func (h *Handler) getUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not found in token"))
		return uuid.Nil, false
	}
	s, ok := raw.(string)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid user context"))
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(s)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid user context"))
		return uuid.Nil, false
	}
	return userID, true
}

// optionalUserID resolves the caller for public endpoints: the
// authenticated user id when a valid token was supplied, uuid.Nil for an
// anonymous request (personal fields then come back empty).
func (h *Handler) optionalUserID(c *gin.Context) uuid.UUID {
	raw, exists := c.Get("user_id")
	if !exists {
		return uuid.Nil
	}
	s, ok := raw.(string)
	if !ok {
		return uuid.Nil
	}
	userID, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return userID
}

func respondError(c *gin.Context, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		api.RespondError(c, appErr)
		return
	}
	api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "internal error", err))
}

// Scan godoc
// @Summary      Scan wallets (public)
// @Description  Offset-paginated wallet scanner with multi-select and metric filters, timeframe windows and deterministic sorting (TEST-01). Public endpoint: a Bearer token personalizes rows (caller's tag, watchlist star); without a token tag=null and watchlisted=false. The watchlisted filter requires authentication (AUTH-003 otherwise).
// @Tags         wallets
// @Produce      json
// @Param        search              query  string  false  "Partial address or own tag match (case-insensitive)"
// @Param        dex                 query  string  false  "DEX filter, data-driven enum (repeat or comma-separated; e.g. hyperliquid,extended; unknown → COMMON-902)"
// @Param        chain               query  string  false  "Chain filter, data-driven enum (repeat or comma-separated; e.g. evm; unknown → COMMON-902)"
// @Param        market              query  string  false  "Market filter, data-driven enum (repeat or comma-separated; e.g. BTC; unknown → COMMON-902)"
// @Param        watchlisted         query  string  false  "Watchlist star filter: true = starred only, false = unstarred only (caller's own stars)" enums(true,false)
// @Param        timeframe           query  string  false  "Metric window" enums(24H,7D,30D,90D,ALL) default(30D)
// @Param        start               query  string  false  "Custom range start (RFC3339, requires end)"
// @Param        end                 query  string  false  "Custom range end (RFC3339)"
// @Param        pnl_gt              query  number  false  "realized_pnl > value"
// @Param        pnl_gte             query  number  false  "realized_pnl >= value"
// @Param        pnl_lt              query  number  false  "realized_pnl < value"
// @Param        pnl_lte             query  number  false  "realized_pnl <= value"
// @Param        pnl_between         query  string  false  "realized_pnl between lo,hi"
// @Param        roi_gt              query  number  false  "roi > value"
// @Param        roi_gte             query  number  false  "roi >= value"
// @Param        roi_lt              query  number  false  "roi < value"
// @Param        roi_lte             query  number  false  "roi <= value"
// @Param        roi_between         query  string  false  "roi between lo,hi"
// @Param        win_rate_gt         query  number  false  "win_rate > value (0-100)"
// @Param        win_rate_gte        query  number  false  "win_rate >= value (0-100)"
// @Param        win_rate_lt         query  number  false  "win_rate < value (0-100)"
// @Param        win_rate_lte        query  number  false  "win_rate <= value (0-100)"
// @Param        win_rate_between    query  string  false  "win_rate between lo,hi"
// @Param        volume_gt           query  number  false  "volume > value"
// @Param        volume_gte          query  number  false  "volume >= value"
// @Param        volume_lt           query  number  false  "volume < value"
// @Param        volume_lte          query  number  false  "volume <= value"
// @Param        volume_between      query  string  false  "volume between lo,hi"
// @Param        trade_count_gt      query  number  false  "trade_count > value"
// @Param        trade_count_gte     query  number  false  "trade_count >= value"
// @Param        trade_count_lt      query  number  false  "trade_count < value"
// @Param        trade_count_lte     query  number  false  "trade_count <= value"
// @Param        trade_count_between query  string  false  "trade_count between lo,hi"
// @Param        avg_position_gt     query  number  false  "avg_position > value"
// @Param        avg_position_gte    query  number  false  "avg_position >= value"
// @Param        avg_position_lt     query  number  false  "avg_position < value"
// @Param        avg_position_lte    query  number  false  "avg_position <= value"
// @Param        avg_position_between query  string  false  "avg_position between lo,hi"
// @Param        avg_leverage_gt     query  number  false  "avg_leverage > value"
// @Param        avg_leverage_gte    query  number  false  "avg_leverage >= value"
// @Param        avg_leverage_lt     query  number  false  "avg_leverage < value"
// @Param        avg_leverage_lte    query  number  false  "avg_leverage <= value"
// @Param        avg_leverage_between query  string  false  "avg_leverage between lo,hi"
// @Param        long_short_ratio_gt query  number  false  "long/short ratio > value"
// @Param        long_short_ratio_gte query  number  false  "long/short ratio >= value"
// @Param        long_short_ratio_lt query  number  false  "long/short ratio < value"
// @Param        long_short_ratio_lte query  number  false  "long/short ratio <= value"
// @Param        long_short_ratio_between query  string  false  "long/short ratio between lo,hi"
// @Param        last_active_within  query  string  false  "Active within duration, e.g. 24h"
// @Param        last_active_from    query  string  false  "Active from (RFC3339)"
// @Param        last_active_to      query  string  false  "Active to (RFC3339)"
// @Param        sort                query  string  false  "Sort field" enums(pnl,roi,win_rate,volume,trade_count,avg_position,avg_leverage,last_active) default(pnl)
// @Param        order               query  string  false  "Sort direction" enums(asc,desc) default(desc)
// @Param        page                query  int     false  "Page" minimum(1) default(1)
// @Param        limit               query  int     false  "Page size" minimum(1) maximum(200) default(50)
// @Success      200  {object}  api.Response{data=[]Wallet,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/wallets [get]
func (h *Handler) Scan(c *gin.Context) {
	userID := h.optionalUserID(c)

	wallets, meta, err := h.service.Scan(c.Request.Context(), userID, c.Request.URL.Query())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: wallets, Meta: meta})
}

// Detail godoc
// @Summary      Wallet detail (public)
// @Description  One wallet with timeframe metrics, per-market positions breakdown (empty array when none) and group memberships. Public endpoint: a Bearer token returns the caller's own tag/star/memberships; without a token those come back empty (BE-06 still never leaks other users' data).
// @Tags         wallets
// @Produce      json
// @Param        id         path   string  true   "Wallet ID"
// @Param        timeframe  query  string  false  "Metric window" enums(24H,7D,30D,90D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=WalletDetail}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/wallets/{id} [get]
func (h *Handler) Detail(c *gin.Context) {
	userID := h.optionalUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondValidationError(c, []api.FieldError{{
			Field:   "id",
			Code:    string(domain.ErrCodeValidation),
			Message: "invalid wallet ID",
		}})
		return
	}

	detail, err := h.service.Detail(c.Request.Context(), userID, id, url.Values{"timeframe": c.QueryArray("timeframe")})
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: detail})
}

// UpdateTagRequest is the body for PATCH /wallets/:id. At least one field
// is required (both nil → 400). A nil tag means "leave the label alone",
// an empty/blank tag clears it; nil watchlisted leaves the star untouched.
type UpdateTagRequest struct {
	Tag         *string `json:"tag"`
	Watchlisted *bool   `json:"watchlisted"`
}

// UpdateTag godoc
// @Summary      Set wallet tag and/or watchlist star
// @Description  Updates the caller's private label and/or watchlist star for a wallet; returns the refreshed detail. Requires authentication. At least one of tag/watchlisted is required (else COMMON-902). Tag is trimmed, max 100 runes; empty string clears the label. watchlisted=true stars, false unstars (per-user, PK upsert). Unknown wallet is WALLET-001. Never conflicts: no 409 exists on this endpoint.
// @Tags         wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string            true  "Wallet ID"
// @Param        request  body      UpdateTagRequest  true  "tag (empty clears) and/or watchlisted"
// @Success      200  {object}  api.Response{data=WalletDetail}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/wallets/{id} [patch]
func (h *Handler) UpdateTag(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondValidationError(c, []api.FieldError{{
			Field:   "id",
			Code:    string(domain.ErrCodeValidation),
			Message: "invalid wallet ID",
		}})
		return
	}

	var req UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{{
			Field:   "tag",
			Code:    string(domain.ErrCodeValidation),
			Message: "tag must be a string and watchlisted must be a boolean",
		}})
		return
	}

	detail, err := h.service.UpdateWallet(c.Request.Context(), userID, id, req.Tag, req.Watchlisted)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: detail})
}

// FilterConfig godoc
// @Summary      Scanner filter config (public)
// @Description  Data-driven filter enums (dexes, chains, markets) plus the code-declared timeframe/sort/metric tables so the frontend renders filters dynamically (CFG-*). Public endpoint — no user data.
// @Tags         wallets
// @Produce      json
// @Success      200  {object}  api.Response{data=ScannerConfig}
// @Router       /api/v1/wallets/filter-config [get]
func (h *Handler) FilterConfig(c *gin.Context) {
	c.JSON(http.StatusOK, api.Response{Success: true, Data: h.service.Config()})
}

// HasScannerParams reports whether q contains any scanner-specific query
// parameter beyond the Phase 1 list params (search/page/limit) — used by the
// group list endpoint to route to the scanner path (BE-09).
func HasScannerParams(q url.Values) bool {
	for key := range q {
		switch strings.ToLower(key) {
		case "search", "page", "limit":
			continue
		default:
			return true
		}
	}
	return false
}
