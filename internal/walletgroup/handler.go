package walletgroup

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/wallet"
)

// Scanner is the group-scoped wallet scanner (BE-09); satisfied by
// *wallet.Service. Declared as an interface to keep both packages free of
// import cycles.
type Scanner interface {
	ScanGroupWallets(ctx context.Context, userID, groupID uuid.UUID, query url.Values) ([]*wallet.GroupWallet, *api.Meta, error)
}

type Handler struct {
	service *Service
	scanner Scanner
	log     *logger.Logger
}

func NewHandler(service *Service, logs ...*logger.Logger) *Handler {
	h := &Handler{service: service}
	if len(logs) > 0 {
		h.log = logs[0]
	}
	return h
}

// logMutation records group mutations with actor + request id (BE-13).
// Only IDs, names and counts are logged — never tokens or secrets.
func (h *Handler) logMutation(c *gin.Context, action string, fields ...any) {
	if h.log == nil {
		return
	}
	args := []any{"module", "walletgroup", "action", action,
		"actor", c.GetString("user_id"), "request_id", c.GetString("request_id")}
	h.log.Info("group mutation", append(args, fields...)...)
}

// SetScanner enables the scanner filter path on GET /groups/:id/wallets;
// without it the endpoint keeps the Phase 1 search/page/limit behavior.
func (h *Handler) SetScanner(s Scanner) {
	h.scanner = s
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	groups := router.Group("/groups")
	if len(jwtMiddleware) > 0 {
		groups.Use(jwtMiddleware[0])
	}
	{
		groups.POST("", h.Create)
		groups.GET("", h.List)
		groups.GET("/:id", h.Get)
		groups.PATCH("/:id", h.Update)
		groups.DELETE("/:id", h.Delete)
		groups.POST("/:id/wallets", h.AddWallets)
		groups.DELETE("/:id/wallets", h.RemoveWallets)
		groups.GET("/:id/wallets", h.ListWallets)
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

func parseGroupID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondValidationError(c, []api.FieldError{{
			Field:   "id",
			Code:    string(domain.ErrCodeValidation),
			Message: "invalid group ID",
		}})
		return uuid.Nil, false
	}
	return id, true
}

func respondServiceError(c *gin.Context, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		api.RespondError(c, appErr)
		return
	}
	api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "internal error", err))
}

func bindingFieldError(field, message string) []api.FieldError {
	return []api.FieldError{{
		Field:   field,
		Code:    string(domain.ErrCodeValidation),
		Message: message,
	}}
}

// Create godoc
// @Summary      Create wallet group
// @Description  Create a wallet group owned by the current user. Duplicate name within the same user is 409 GROUP-002.
// @Tags         groups
// @Accept       json
// @Produce      json
// @Param        request  body      CreateGroupRequest  true  "Group to create"
// @Success      201      {object}  api.Response{data=Group}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      401      {object}  api.Response{error=api.ErrorBody}
// @Failure      409      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups [post]
func (h *Handler) Create(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}

	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, bindingFieldError("name", "name is required"))
		return
	}
	if _, fieldErr := ValidateGroupName(req.Name); fieldErr != nil {
		api.RespondValidationError(c, []api.FieldError{*fieldErr})
		return
	}

	g, err := h.service.CreateGroup(c.Request.Context(), userID, &req)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	h.logMutation(c, "group.create", "group_id", g.ID.String(), "name", g.Name)
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: g})
}

// List godoc
// @Summary      List wallet groups
// @Description  List the current user's wallet groups with wallet counts
// @Tags         groups
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Group}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups [get]
func (h *Handler) List(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}

	groups, err := h.service.ListGroups(c.Request.Context(), userID)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{Success: true, Data: groups})
}

// Get godoc
// @Summary      Get wallet group
// @Description  Get one group with its wallet count
// @Tags         groups
// @Produce      json
// @Param        id   path      string  true  "Group ID"
// @Success      200  {object}  api.Response{data=Group}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	g, err := h.service.GetGroup(c.Request.Context(), userID, groupID)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{Success: true, Data: g})
}

// Update godoc
// @Summary      Update wallet group
// @Description  Update name/description/color of an owned group. Renaming to an existing name of the same user is 409 GROUP-002.
// @Tags         groups
// @Accept       json
// @Produce      json
// @Param        id       path      string              true  "Group ID"
// @Param        request  body      UpdateGroupRequest  true  "Fields to update"
// @Success      200      {object}  api.Response{data=Group}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      403      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Failure      409      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id} [patch]
func (h *Handler) Update(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, bindingFieldError("body", "invalid JSON body"))
		return
	}
	if req.Name != nil {
		if _, fieldErr := ValidateGroupName(*req.Name); fieldErr != nil {
			api.RespondValidationError(c, []api.FieldError{*fieldErr})
			return
		}
	}

	g, err := h.service.UpdateGroup(c.Request.Context(), userID, groupID, &req)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	h.logMutation(c, "group.update", "group_id", groupID.String())
	c.JSON(http.StatusOK, api.Response{Success: true, Data: g})
}

// Delete godoc
// @Summary      Delete wallet group
// @Description  Delete an owned group; memberships are removed, wallets stay
// @Tags         groups
// @Produce      json
// @Param        id   path      string  true  "Group ID"
// @Success      204
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	if err := h.service.DeleteGroup(c.Request.Context(), userID, groupID); err != nil {
		respondServiceError(c, err)
		return
	}

	h.logMutation(c, "group.delete", "group_id", groupID.String())
	c.JSON(http.StatusNoContent, nil)
}

// AddWallets godoc
// @Summary      Add wallets to group
// @Description  Idempotently add wallet IDs or addresses to an owned group
// @Tags         groups
// @Accept       json
// @Produce      json
// @Param        id       path      string           true  "Group ID"
// @Param        request  body      WalletsRequest   true  "Wallet entries (IDs or addresses)"
// @Success      200      {object}  api.Response{data=AddWalletsResult}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      403      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id}/wallets [post]
func (h *Handler) AddWallets(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	var req WalletsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, bindingFieldError("wallets", "wallets must be a non-empty array"))
		return
	}

	created, err := h.service.AddWallets(c.Request.Context(), userID, groupID, &req)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	// All-or-nothing transaction: success means every entry resolved, so
	// skipped = entries already members (idempotent re-adds). Failures
	// surface as error responses, never as partial counts.
	skipped := int64(len(req.Wallets)) - created
	if skipped < 0 {
		skipped = 0
	}
	h.logMutation(c, "group.wallets.add", "group_id", groupID.String(), "added", created, "skipped", skipped)
	c.JSON(http.StatusOK, api.Response{Success: true, Data: AddWalletsResult{Added: created, Skipped: skipped}})
}

// RemoveWallets godoc
// @Summary      Remove wallets from group
// @Description  Idempotently remove wallet IDs or addresses from an owned group
// @Tags         groups
// @Accept       json
// @Produce      json
// @Param        id       path      string           true  "Group ID"
// @Param        request  body      WalletsRequest   true  "Wallet entries (IDs or addresses)"
// @Success      204
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      403      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id}/wallets [delete]
func (h *Handler) RemoveWallets(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	var req WalletsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, bindingFieldError("wallets", "wallets must be a non-empty array"))
		return
	}

	if _, err := h.service.RemoveWallets(c.Request.Context(), userID, groupID, &req); err != nil {
		respondServiceError(c, err)
		return
	}

	h.logMutation(c, "group.wallets.remove", "group_id", groupID.String())
	c.JSON(http.StatusNoContent, nil)
}

// ListWallets godoc
// @Summary      List group wallets
// @Description  Paginated wallets of a group with partial address search.
// @Description  Always returns data=[]wallet.GroupWallet: every Wallet field (id,chain,address,dex,tag,first_seen_at,last_seen_at,metrics) plus membership added_at. Metrics/tag are null when absent.
// @Description  Pass include=metrics or any scanner filter param (dex/chain/market/timeframe/metric operators/sort) for metric-enriched rows (BE-09).
// @Tags         groups
// @Produce      json
// @Param        id       path   string  true   "Group ID"
// @Param        search   query  string  false  "Partial address or own tag match (case-insensitive)"
// @Param        page     query  int     false  "Page" minimum(1) default(1)
// @Param        limit    query  int     false  "Page size" minimum(1) maximum(200) default(50)
// @Param        include  query  string  false  "Set to metrics for metric-enriched rows" enums(metrics)
// @Param        dex                 query  string  false  "DEX filter, data-driven enum (repeat or comma-separated)"
// @Param        chain               query  string  false  "Chain filter, data-driven enum (repeat or comma-separated)"
// @Param        market              query  string  false  "Market filter, data-driven enum (repeat or comma-separated)"
// @Param        timeframe           query  string  false  "Metric window" enums(24H,7D,30D,90D,ALL) default(30D)
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
// @Success      200     {object}  api.Response{data=[]wallet.GroupWallet,meta=api.Meta}
// @Failure      400     {object}  api.Response{error=api.ErrorBody}
// @Failure      403     {object}  api.Response{error=api.ErrorBody}
// @Failure      404     {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/groups/{id}/wallets [get]
func (h *Handler) ListWallets(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groupID, ok := parseGroupID(c)
	if !ok {
		return
	}

	// Scanner path (BE-09): include=metrics or any non-Phase-1 query param
	// triggers the metric-enriched group scanner; otherwise keep the
	// lightweight Phase 1 behavior.
	q := c.Request.URL.Query()
	if h.scanner != nil && (q.Get("include") == "metrics" || wallet.HasScannerParams(q)) {
		wallets, meta, err := h.scanner.ScanGroupWallets(c.Request.Context(), userID, groupID, q)
		if err != nil {
			respondServiceError(c, err)
			return
		}
		c.JSON(http.StatusOK, api.Response{Success: true, Data: wallets, Meta: meta})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	search := strings.TrimSpace(c.Query("search"))

	wallets, meta, err := h.service.ListGroupWallets(c.Request.Context(), userID, groupID, search, page, limit)
	if err != nil {
		respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{Success: true, Data: wallets, Meta: meta})
}
