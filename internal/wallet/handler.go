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
		wallets.GET("/:id", h.Detail)
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

func respondError(c *gin.Context, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		api.RespondError(c, appErr)
		return
	}
	api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "internal error", err))
}

// Scan godoc
// @Summary      Scan wallets
// @Description  Offset-paginated wallet scanner with multi-select and metric
//
//	filters, timeframe windows and deterministic sorting (TEST-01)
//
// @Tags         wallets
// @Produce      json
// @Param        search              query  string  false  "Partial address match"
// @Param        dex                 query  string  false  "DEX filter (repeat or comma-separated)"
// @Param        chain               query  string  false  "Chain filter (repeat or comma-separated)"
// @Param        market              query  string  false  "Market filter (repeat or comma-separated)"
// @Param        timeframe           query  string  false  "24H|7D|30D|90D|ALL (default 30D)"
// @Param        start               query  string  false  "Custom range start (RFC3339, requires end)"
// @Param        end                 query  string  false  "Custom range end (RFC3339)"
// @Param        pnl_gt              query  number  false  "realized_pnl > value"
// @Param        pnl_gte             query  number  false  "realized_pnl >= value"
// @Param        pnl_lt              query  number  false  "realized_pnl < value"
// @Param        pnl_lte             query  number  false  "realized_pnl <= value"
// @Param        pnl_between         query  string  false  "realized_pnl between lo,hi"
// @Param        roi_gt              query  number  false  "roi > value"
// @Param        roi_between         query  string  false  "roi between lo,hi"
// @Param        win_rate_gte        query  number  false  "win_rate >= value"
// @Param        volume_gt           query  number  false  "volume > value"
// @Param        trade_count_gte     query  number  false  "trade_count >= value"
// @Param        avg_position_gt     query  number  false  "avg_position > value"
// @Param        avg_leverage_lte    query  number  false  "avg_leverage <= value"
// @Param        long_short_ratio_gt query  number  false  "long/short ratio > value"
// @Param        last_active_within  query  string  false  "Active within duration, e.g. 24h"
// @Param        last_active_from    query  string  false  "Active from (RFC3339)"
// @Param        last_active_to      query  string  false  "Active to (RFC3339)"
// @Param        sort                query  string  false  "pnl|roi|win_rate|volume|trade_count|avg_position|avg_leverage|last_active"
// @Param        order               query  string  false  "asc|desc (default desc)"
// @Param        page                query  int     false  "Page (default 1)"
// @Param        limit               query  int     false  "Page size (default 50, max 200)"
// @Success      200  {object}  api.Response{data=[]Wallet,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/wallets [get]
func (h *Handler) Scan(c *gin.Context) {
	if _, ok := h.getUserID(c); !ok {
		return
	}

	wallets, meta, err := h.service.Scan(c.Request.Context(), c.Request.URL.Query())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: wallets, Meta: meta})
}

// Detail godoc
// @Summary      Wallet detail
// @Description  One wallet with timeframe metrics and the caller's own group
//
//	memberships only (BE-06)
//
// @Tags         wallets
// @Produce      json
// @Param        id         path   string  true   "Wallet ID"
// @Param        timeframe  query  string  false  "24H|7D|30D|90D|ALL (default 30D)"
// @Success      200  {object}  api.Response{data=WalletDetail}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/wallets/{id} [get]
func (h *Handler) Detail(c *gin.Context) {
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

	detail, err := h.service.Detail(c.Request.Context(), userID, id, url.Values{"timeframe": c.QueryArray("timeframe")})
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: detail})
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
