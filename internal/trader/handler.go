package trader

import (
	"context"
	"errors"
	"net/http"
	"strconv"

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
	Positions(ctx context.Context, venueCode, addr, sort, dir string) (*PositionSnapshotDTO, error)
	Activity(ctx context.Context, venueCode, addr string, q ActivityQuery) (*ActivityPage, error)
	Balances(ctx context.Context, venueCode, addr string) (*BalancesDTO, error)
	Fills(ctx context.Context, venueCode, addr string, limit int, cursor string) (*FillsPage, error)
	Orders(ctx context.Context, venueCode, addr, status string, limit int) (*OrdersDTO, error)
	Transfers(ctx context.Context, venueCode, addr string, days, limit int, cursor string) (*TransfersPage, error)
	Performance(ctx context.Context, venueCode, addr, period string) (*PerformanceDTO, error)
}

var _ ServiceInterface = (*Service)(nil)

// Handler serves the public v1.1 scanner reads.
type Handler struct {
	svc ServiceInterface
	ws  *ActivityWSHandler
}

func NewHandler(svc ServiceInterface) *Handler { return &Handler{svc: svc} }

// WithWS attaches the realtime activity stream (DETAIL-PLAN A8). Nil keeps
// the REST-only surface (unit/E2E routers without a hub).
func (h *Handler) WithWS(ws *ActivityWSHandler) *Handler {
	h.ws = ws
	return h
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, _ ...gin.HandlerFunc) {
	t := router.Group("/traders")
	t.POST("/search", h.Search)
	// Static routes before param routes (DETAIL-PLAN A8: /ws must win over /:wallet).
	if h.ws != nil {
		t.GET("/ws", h.ws.ServeWS)
	}
	t.GET("/:wallet/positions", h.Positions)
	t.GET("/:wallet/activity", h.Activity)
	t.GET("/:wallet/balances", h.Balances)
	t.GET("/:wallet/fills", h.Fills)
	t.GET("/:wallet/orders", h.Orders)
	t.GET("/:wallet/transfers", h.Transfers)
	t.GET("/:wallet/performance", h.Performance)
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
// @Description  Trader scan over period metrics (spec v1.1). Two additive pagination modes: legacy keyset cursor (no page field; next page via meta.cursor/has_more) and numbered pages (page >= 1 selects LIMIT/OFFSET with offset=(page-1)*limit; a cursor in the same body is ignored; meta carries page/total/total_pages with total_pages=ceil(total/limit)). Public endpoint: group_id requires a token (AUTH-003 otherwise). Reads period_metrics only, never upstream.
// @Tags         traders
// @Accept       json
// @Produce      json
// @Param        request  body      SearchRequest  true  "period, venue, min/max filters, sort, limit, cursor, page"
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
		Page: res.Page, Total: res.Total, TotalPages: res.TotalPages,
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

// Positions godoc
// @Summary      Trader open positions (public)
// @Description  Latest open-position snapshot for a watched wallet (DETAIL-PLAN §4.1 + WALLET-TABS v1 §1.1). Never calls upstream inline; freshness from last_positions_sync_at (NULL/never synced reports data_status=syncing per M5). Summary null when never synced; positions is [] (never null). Server-side sort.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        sort    query  string  false  "Sort column" enums(coin,size,entry_price,mark_price,position_value,unrealized_pnl,return_on_equity,leverage) default(coin)
// @Param        dir     query  string  false  "Sort direction" enums(asc,desc) default(asc)
// @Success      200  {object}  api.Response{data=PositionSnapshotDTO}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER / validation"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/positions [get]
func (h *Handler) Positions(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	d, err := h.svc.Positions(c.Request.Context(), venue, c.Param("wallet"), c.Query("sort"), c.Query("dir"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, d)
}

// Activity godoc
// @Summary      Trader recent activity (public)
// @Description  Durable closed trades with server-computed net_pnl (WALLET-TABS v1 §1.2). Server-side sort/filter; keyset pagination honouring sort+dir; cursor opaque + HMAC-sealed over (sortKey, closed_at, market, opened_at). Counts across the whole retained window. Default limit=20 (1..100), sort=closed_at, dir=desc, result=all, side=all.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        limit   query  int     false  "Page size 1..100" default(20)
// @Param        cursor  query  string  false  "Opaque page cursor"
// @Param        sort    query  string  false  "Sort column" enums(closed_at,opened_at,market,volume,pnl,net_pnl,duration,entry_price,exit_price) default(closed_at)
// @Param        dir     query  string  false  "Sort direction" enums(asc,desc) default(desc)
// @Param        result  query  string  false  "Result filter" enums(all,win,loss) default(all)
// @Param        side    query  string  false  "Side filter" enums(all,long,short) default(all)
// @Success      200  {object}  api.Response{data=ActivityPage}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER (limit/cursor/sort/dir/result/side)"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/activity [get]
func (h *Handler) Activity(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100"))
			return
		}
		limit = n
	}
	q := ActivityQuery{
		Limit: limit, Cursor: c.Query("cursor"),
		Sort: c.Query("sort"), Dir: c.Query("dir"),
		Result: c.Query("result"), Side: c.Query("side"),
	}
	page, err := h.svc.Activity(c.Request.Context(), venue, c.Param("wallet"), q)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: page, Meta: &api.Meta{Limit: limit, HasMore: page.HasMore, Cursor: page.NextCursor}})
}

// Balances godoc
// @Summary      Trader balances (public, on-demand)
// @Description  Perp + spot balances on-demand with short TTL cache (WALLET-TABS v1 §1.3, WS2). Either side may be null on partial failure; data_status reflects it. No DB.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Success      200  {object}  api.Response{data=BalancesDTO}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER / validation"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/balances [get]
func (h *Handler) Balances(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	d, err := h.svc.Balances(c.Request.Context(), venue, c.Param("wallet"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, d)
}

// Fills godoc
// @Summary      Trader fills (public, on-demand)
// @Description  Recent user fills newest-first with tid keyset pagination (WALLET-TABS v1 §1.4, WS4). Source userFills (2000 most recent); FE paginates through the fetched set. Short TTL cache; graceful degradation on HL error.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        limit   query  int     false  "Page size 1..200" default(100)
// @Param        cursor  query  string  false  "Opaque page cursor (by tid)"
// @Success      200  {object}  api.Response{data=FillsPage}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER (limit/cursor)"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/fills [get]
func (h *Handler) Fills(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..200"))
			return
		}
		limit = n
	}
	page, err := h.svc.Fills(c.Request.Context(), venue, c.Param("wallet"), limit, c.Query("cursor"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, page)
}

// Orders godoc
// @Summary      Trader orders (public, on-demand)
// @Description  Open or historical orders (WALLET-TABS v1 §1.5, WS3). order_status/status_timestamp present only for status=historical. Short TTL cache.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        status  query  string  false  "Order set" enums(open,historical) default(open)
// @Param        limit   query  int     false  "Max rows 1..2000" default(200)
// @Success      200  {object}  api.Response{data=OrdersDTO}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER (status/limit)"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/orders [get]
func (h *Handler) Orders(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	status := c.DefaultQuery("status", "open")
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 2000 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..2000"))
			return
		}
		limit = n
	}
	d, err := h.svc.Orders(c.Request.Context(), venue, c.Param("wallet"), status, limit)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, d)
}

// Transfers godoc
// @Summary      Trader transfers (public, on-demand)
// @Description  Non-funding ledger transfers newest-first with time+hash keyset pagination (WALLET-TABS v1 §1.6, WS5). Funding excluded by decision. Short TTL cache.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        days    query  int     false  "Lookback 1..180" default(30)
// @Param        limit   query  int     false  "Page size 1..500" default(200)
// @Param        cursor  query  string  false  "Opaque page cursor (by time+hash)"
// @Success      200  {object}  api.Response{data=TransfersPage}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER (days/limit/cursor)"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/transfers [get]
func (h *Handler) Transfers(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	days := 30
	if raw := c.Query("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 180 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "days must be 1..180"))
			return
		}
		days = n
	}
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..500"))
			return
		}
		limit = n
	}
	page, err := h.svc.Transfers(c.Request.Context(), venue, c.Param("wallet"), days, limit, c.Query("cursor"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, page)
}

// Performance godoc
// @Summary      Trader performance (public)
// @Description  Period metrics + equity curve (WALLET-TABS v1 §1.7, WS6). Reuses Detail metrics + trader_equity_daily; no new source.
// @Tags         traders
// @Produce      json
// @Param        wallet  path   string  true   "Wallet address (0x...)"
// @Param        venue   query  string  false  "Venue code" default(hyperliquid)
// @Param        period  query  string  false  "Metric window" enums(1D,7D,30D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=PerformanceDTO}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "INVALID_FILTER / validation"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "unknown wallet/venue"
// @Router       /api/v1/traders/{wallet}/performance [get]
func (h *Handler) Performance(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := c.DefaultQuery("period", Period30D)
	d, err := h.svc.Performance(c.Request.Context(), venue, c.Param("wallet"), period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, d)
}
