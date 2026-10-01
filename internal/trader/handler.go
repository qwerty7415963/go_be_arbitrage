package trader

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// ServiceInterface is the scanner read surface (mocked in handler tests;
// *Service implements it).
type ServiceInterface interface {
	Search(ctx context.Context, userID uuid.UUID, req *SearchRequest) (*SearchResult, error)
	Detail(ctx context.Context, venueCode, addr, period string) (*Detail, error)
}

var _ ServiceInterface = (*Service)(nil)

// Handler serves the public v1.1 scanner reads.
type Handler struct {
	svc ServiceInterface
}

func NewHandler(svc ServiceInterface) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, _ ...gin.HandlerFunc) {
	t := router.Group("/traders")
	t.POST("/search", h.Search)
	t.GET("/:wallet", h.Detail)
}

// optionalUserID returns uuid.Nil for anonymous callers (public reads).
func optionalUserID(c *gin.Context) uuid.UUID {
	raw, ok := c.Get("user_id")
	if !ok {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw.(string))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func appErr(err error) *domain.AppError {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return domain.NewError(domain.ErrCodeInternal, "internal error").WithErr(err)
}

// Search godoc
// @Summary      Search traders (public)
// @Description  Cursor-paginated trader scan over period metrics (spec v1.1). Public endpoint: group_id requires a token (AUTH-003 otherwise). Reads period_metrics only, never upstream.
// @Tags         traders
// @Accept       json
// @Produce      json
// @Param        request  body      SearchRequest  true  "period, venue, min/max filters, sort, limit, cursor"
// @Success      200  {object}  api.Response{data=[]PeriodMetrics,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003 group filter without token"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "GROUP-003 foreign group"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown group"
// @Router       /api/v1/traders/search [post]
func (h *Handler) Search(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid search body"))
		return
	}
	res, err := h.svc.Search(c.Request.Context(), optionalUserID(c), &req)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: res.Rows, Meta: &api.Meta{
		Limit: req.Limit, HasMore: res.HasMore, Cursor: res.NextCursor,
	}})
}

// Detail godoc
// @Summary      Trader detail (public)
// @Description  Registry header + one period's metrics by wallet address (period query, default 30D; venue query, default hyperliquid). Unknown wallet is 404. Never calls upstream inline.
// @Tags         traders
// @Produce      json
// @Param        wallet   path   string  true   "Wallet address (0x...)"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        period   query  string  false  "Metric window" enums(1D,7D,30D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=Detail}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER / validation"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet} [get]
func (h *Handler) Detail(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := c.DefaultQuery("period", Period30D)
	d, err := h.svc.Detail(c.Request.Context(), venue, c.Param("wallet"), period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, d)
}
