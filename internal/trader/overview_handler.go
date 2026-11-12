package trader

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Overview godoc
// @Summary      Trader overview (public)
// @Description  Aggregate trader overview buckets (PnL + account) with series.
// @Tags         traders
// @Produce      json
// @Param        venue        query  string  false  "Venue code" default(hyperliquid)
// @Param        period       query  string  false  "Period" enums(1D,7D,30D,ALL) default(30D)
// @Param        pnl_metric   query  string  false  "PnL metric" enums(pnl,realized_pnl) default(realized_pnl)
// @Param        series_days  query  int     false  "Series days 7..90" default(30)
// @Success      200  {object}  api.Response{data=TraderOverview}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/overview [get]
func (h *Handler) Overview(c *gin.Context) {
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("period", Period30D)))
	pnlMetric := c.DefaultQuery("pnl_metric", "realized_pnl")
	seriesDays := 30
	if raw := c.Query("series_days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 7 || n > 90 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "series_days must be 7..90"))
			return
		}
		seriesDays = n
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period"))
		return
	}
	if pnlMetric != "pnl" && pnlMetric != "realized_pnl" {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid pnl_metric"))
		return
	}
	d, err := h.svc.Overview(c.Request.Context(), venue, period, pnlMetric, seriesDays)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: d})
}

// BucketDetail godoc
// @Summary      Bucket detail (public)
// @Tags         traders
// @Produce      json
// @Param        kind     path   string  true   "Bucket kind" enums(pnl,account)
// @Param        id       path   string  true   "Bucket id"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        period   query  string  false  "Period" enums(1D,7D,30D,ALL) default(30D)
// @Param        range    query  string  false  "Range" enums(24H,7D,30D) default(30D)
// @Success      200  {object}  api.Response{data=BucketDetail}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/overview/buckets/{kind}/{id} [get]
func (h *Handler) BucketDetail(c *gin.Context) {
	kind := c.Param("kind")
	id := c.Param("id")
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("period", Period30D)))
	rangeCode := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("range", "30D")))
	d, err := h.svc.BucketDetail(c.Request.Context(), venue, kind, id, period, rangeCode)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: d})
}

// BucketWallets godoc
// @Summary      Bucket wallets (public)
// @Tags         traders
// @Produce      json
// @Param        kind     path   string  true   "Bucket kind" enums(pnl,account)
// @Param        id       path   string  true   "Bucket id"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        period   query  string  false  "Period" enums(1D,7D,30D,ALL) default(30D)
// @Param        sort     query  string  false  "Sort" enums(equity,win_rate,volume,pnl,bias) default(equity)
// @Param        dir      query  string  false  "Direction" enums(asc,desc) default(desc)
// @Param        page     query  int     false  "Page" default(1)
// @Param        limit    query  int     false  "Limit 1..100" default(20)
// @Success      200  {object}  api.Response{data=[]BucketWalletRow,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/overview/buckets/{kind}/{id}/wallets [get]
func (h *Handler) BucketWallets(c *gin.Context) {
	kind := c.Param("kind")
	id := c.Param("id")
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("period", Period30D)))
	sort := c.DefaultQuery("sort", "equity")
	dir := c.DefaultQuery("dir", "desc")
	page := 1
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
			return
		}
		page = n
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100"))
			return
		}
		limit = n
	}
	rows, total, err := h.svc.BucketWallets(c.Request.Context(), venue, kind, id, period, sort, dir, page, limit)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}
	hasMore := page < totalPages
	c.JSON(http.StatusOK, api.Response{Success: true, Data: rows, Meta: &api.Meta{
		Page: page, Total: int64(total), TotalPages: totalPages, Limit: limit, HasMore: hasMore,
	}})
}

// BucketMarkets godoc
// @Summary      Bucket markets (public)
// @Tags         traders
// @Produce      json
// @Param        kind     path   string  true   "Bucket kind" enums(pnl,account)
// @Param        id       path   string  true   "Bucket id"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        period   query  string  false  "Period" enums(1D,7D,30D,ALL) default(30D)
// @Param        range    query  string  false  "Range" enums(24H,7D,30D) default(30D)
// @Param        search   query  string  false  "Search market"
// @Param        sort     query  string  false  "Sort" enums(notional,upnl,traders) default(notional)
// @Param        dir      query  string  false  "Direction" enums(asc,desc) default(desc)
// @Param        page     query  int     false  "Page" default(1)
// @Param        limit    query  int     false  "Limit 1..100" default(20)
// @Success      200  {object}  api.Response{data=[]BucketMarketRow,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/overview/buckets/{kind}/{id}/markets [get]
func (h *Handler) BucketMarkets(c *gin.Context) {
	kind := c.Param("kind")
	id := c.Param("id")
	venue := c.DefaultQuery("venue", DefaultVenue)
	period := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("period", Period30D)))
	rangeCode := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("range", "30D")))
	search := c.Query("search")
	sort := c.DefaultQuery("sort", "notional")
	dir := c.DefaultQuery("dir", "desc")
	page := 1
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
			return
		}
		page = n
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..100"))
			return
		}
		limit = n
	}
	rows, total, err := h.svc.BucketMarkets(c.Request.Context(), venue, kind, id, period, rangeCode, search, sort, dir, page, limit)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}
	hasMore := page < totalPages
	c.JSON(http.StatusOK, api.Response{Success: true, Data: rows, Meta: &api.Meta{
		Page: page, Total: int64(total), TotalPages: totalPages, Limit: limit, HasMore: hasMore,
	}})
}
