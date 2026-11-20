package trader

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// parseDateParam parses YYYY-MM-DD or RFC3339; returns zero time on failure.
func parseDateParam(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, domain.NewError(domain.ErrCodeInvalidFilter, "invalid date: "+raw)
}

func parseOptionalFloat(raw string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid number")
	}
	return &v, nil
}

func parseOptionalInt(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid integer")
	}
	return &v, nil
}

func parseOptionalTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		v := t.UTC()
		return &v, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		v := t.UTC()
		return &v, nil
	}
	return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid date")
}

func parseUUIDList(raw string) ([]uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := uuid.Parse(p)
		if err != nil {
			return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid group uuid")
		}
		out = append(out, id)
	}
	return out, nil
}

// MarketDetail godoc
// @Summary      Market detail (public)
// @Tags         traders
// @Produce      json
// @Param        market   path   string  true   "Market (coin)"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        from     query  string  false  "From date (YYYY-MM-DD)"
// @Param        to       query  string  false  "To date (YYYY-MM-DD)"
// @Success      200  {object}  api.Response{data=MarketDetailResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/markets/{market} [get]
func (h *Handler) MarketDetail(c *gin.Context) {
	market := c.Param("market")
	venue := c.DefaultQuery("venue", DefaultVenue)
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now
	if raw := c.Query("from"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			from = t
		}
	}
	if raw := c.Query("to"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			to = t
		}
	}
	d, err := h.svc.MarketDetail(c.Request.Context(), venue, market, from, to)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: d})
}

// MarketWallets godoc
// @Summary      Market wallets (public)
// @Tags         traders
// @Produce      json
// @Param        market   path   string  true   "Market (coin)"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        from     query  string  false  "From date (YYYY-MM-DD)"
// @Param        to       query  string  false  "To date (YYYY-MM-DD)"
// @Param        win_rate_min  query  number  false  "Min win rate 0..100"
// @Param        trades_min    query  int     false  "Min trades"
// @Param        last_trade_after query string false "Last trade after date"
// @Param        groups   query  string  false  "Comma-separated group UUIDs"
// @Param        sort     query  string  false  "Sort" default(volume)
// @Param        dir      query  string  false  "Direction" default(desc)
// @Param        page     query  int     false  "Page" default(1)
// @Param        limit    query  int     false  "Limit" default(20)
// @Success      200  {object}  api.Response{data=[]MarketWalletRow,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/markets/{market}/wallets [get]
func (h *Handler) MarketWallets(c *gin.Context) {
	market := c.Param("market")
	venue := c.DefaultQuery("venue", DefaultVenue)
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now
	if raw := c.Query("from"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			from = t
		}
	}
	if raw := c.Query("to"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			to = t
		}
	}
	winRateMin, err := parseOptionalFloat(c.Query("win_rate_min"))
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}
	tradesMin, err := parseOptionalInt(c.Query("trades_min"))
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}
	lastTradeAfter, err := parseOptionalTime(c.Query("last_trade_after"))
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}
	groups, err := parseUUIDList(c.Query("groups"))
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}
	sort := c.DefaultQuery("sort", "volume")
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
	resp, err := h.svc.MarketWallets(c.Request.Context(), venue, market, from, to, winRateMin, tradesMin, lastTradeAfter, groups, sort, dir, page, limit)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if resp.Data == nil {
		resp.Data = []MarketWalletRow{}
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: resp.Data, Meta: resp.Meta})
}

// MarketTrades godoc
// @Summary      Market trades (public)
// @Tags         traders
// @Produce      json
// @Param        market   path   string  true   "Market (coin)"
// @Param        venue    query  string  false  "Venue code" default(hyperliquid)
// @Param        from     query  string  false  "From date (YYYY-MM-DD)"
// @Param        to       query  string  false  "To date (YYYY-MM-DD)"
// @Param        side     query  string  false  "Side" enums(LONG,SHORT,all) default(all)
// @Param        page     query  int     false  "Page" default(1)
// @Param        limit    query  int     false  "Limit" default(20)
// @Success      200  {object}  api.Response{data=[]MarketTradeRow,meta=api.Meta}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/traders/markets/{market}/trades [get]
func (h *Handler) MarketTrades(c *gin.Context) {
	market := c.Param("market")
	venue := c.DefaultQuery("venue", DefaultVenue)
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now
	if raw := c.Query("from"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			from = t
		}
	}
	if raw := c.Query("to"); raw != "" {
		t, err := parseDateParam(raw)
		if err != nil {
			api.RespondError(c, err.(*domain.AppError))
			return
		}
		if !t.IsZero() {
			to = t
		}
	}
	side := c.DefaultQuery("side", "all")
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
	resp, err := h.svc.MarketTrades(c.Request.Context(), venue, market, from, to, side, page, limit)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if resp.Data == nil {
		resp.Data = []MarketTradeRow{}
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: resp.Data, Meta: resp.Meta})
}

// Admin watched markets
type createWatchedMarketReq struct {
	Venue     string `json:"venue"`
	Market    string `json:"market"`
	Enabled   *bool  `json:"enabled"`
	SortOrder *int   `json:"sort_order"`
}

type updateWatchedMarketReq struct {
	Enabled   *bool `json:"enabled"`
	SortOrder *int  `json:"sort_order"`
}

func (h *Handler) ListWatchedMarkets(c *gin.Context) {
	rows, err := h.svc.ListWatchedMarkets(c.Request.Context())
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if rows == nil {
		rows = []WatchedMarket{}
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: rows})
}

func (h *Handler) CreateWatchedMarket(c *gin.Context) {
	var req createWatchedMarketReq
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid body"))
		return
	}
	venue := req.Venue
	if venue == "" {
		venue = DefaultVenue
	}
	market := strings.ToUpper(strings.TrimSpace(req.Market))
	if market == "" {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "market is required"))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	if err := h.svc.CreateWatchedMarket(c.Request.Context(), venue, market, enabled, sortOrder); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true})
}

func (h *Handler) UpdateWatchedMarket(c *gin.Context) {
	market := strings.ToUpper(strings.TrimSpace(c.Param("market")))
	venue := c.DefaultQuery("venue", DefaultVenue)
	var req updateWatchedMarketReq
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid body"))
		return
	}
	if err := h.svc.UpdateWatchedMarket(c.Request.Context(), venue, market, req.Enabled, req.SortOrder); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true})
}

func (h *Handler) DeleteWatchedMarket(c *gin.Context) {
	market := strings.ToUpper(strings.TrimSpace(c.Param("market")))
	venue := c.DefaultQuery("venue", DefaultVenue)
	if err := h.svc.DeleteWatchedMarket(c.Request.Context(), venue, market); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true})
}
