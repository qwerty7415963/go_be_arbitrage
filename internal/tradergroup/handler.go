package tradergroup

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// Store is the group persistence surface (*Repository implements it; mocked
// in handler tests).
type Store interface {
	Create(ctx context.Context, userID uuid.UUID, name, desc string) (*Group, error)
	List(ctx context.Context, userID uuid.UUID, params ListGroupsParams) ([]*GroupListItem, int, error)
	Get(ctx context.Context, groupID, userID uuid.UUID) (*Group, error)
	Update(ctx context.Context, groupID, userID uuid.UUID, name, desc *string, isFavorite, notifyEnabled *bool) (*Group, error)
	Delete(ctx context.Context, groupID, userID uuid.UUID) error
	AddMembers(ctx context.Context, groupID, userID uuid.UUID, items []MemberInput) (int64, int64, error)
	RemoveMembers(ctx context.Context, groupID, userID, venueID uuid.UUID, addrs []string) (int64, error)
	UpdateMembers(ctx context.Context, groupID, userID uuid.UUID, items []ValidatedMemberUpdate) (int64, error)
	ListMembers(ctx context.Context, groupID, userID uuid.UUID, params ListMembersParams) ([]*MemberRow, int, error)
	Activity(ctx context.Context, groupID, userID uuid.UUID, f ActivityFilter) (*ActivityPage, error)
	ListAlerts(ctx context.Context, groupID, userID uuid.UUID) ([]*GroupAlert, error)
	CreateAlert(ctx context.Context, groupID, userID uuid.UUID, req CreateAlertRequest) (*GroupAlert, error)
	UpdateAlert(ctx context.Context, alertID, userID uuid.UUID, req UpdateAlertRequest) (*GroupAlert, error)
	DeleteAlert(ctx context.Context, alertID, userID uuid.UUID) error
	OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error)
	VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error)
	Duplicate(ctx context.Context, groupID, userID uuid.UUID, name *string) (*Group, error)
	ToggleFavorite(ctx context.Context, groupID, userID uuid.UUID, isFavorite bool) (*Group, error)
	Overview(ctx context.Context, userID uuid.UUID, period string) (*OverviewStats, error)
	Stats(ctx context.Context, groupID, userID uuid.UUID, period string) (*GroupStats, error)
	Positions(ctx context.Context, groupID, userID uuid.UUID) (*PositionsResponse, error)
	Exposure(ctx context.Context, groupID, userID uuid.UUID, period string) (*ExposureResponse, error)
	Performance(ctx context.Context, groupID, userID uuid.UUID, period string) (*PerformanceResponse, error)
	Compare(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID, period string) (*CompareResponse, error)
}

var _ Store = (*Repository)(nil)

// Handler serves the JWT-gated v1.1 trader-groups API.
type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler { return &Handler{store: store} }

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	g := router.Group("/trader-groups")
	if len(jwtMiddleware) > 0 {
		g.Use(jwtMiddleware[0])
	}
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/overview", h.Overview)
	// Static /compare must be registered before the /:id wildcard routes.
	g.POST("/compare", h.Compare)
	g.GET("/:id", h.Get)
	g.PATCH("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	g.POST("/:id/duplicate", h.Duplicate)
	g.PATCH("/:id/favorite", h.Favorite)
	g.POST("/:id/members", h.AddMembers)
	g.GET("/:id/members", h.ListMembers)
	g.GET("/:id/alerts", h.ListAlerts)
	g.POST("/:id/alerts", h.CreateAlert)
	g.GET("/:id/alert-events", h.ListAlertEvents)
	g.POST("/:id/alert-events/read-all", h.MarkAllAlertEventsRead)
	g.GET("/alerts/unread-count", h.UnreadAlertCount)
	g.PATCH("/:id/members", h.UpdateMembers)
	g.DELETE("/:id/members", h.RemoveMembers)
	g.PATCH("/alerts/:alertId", h.UpdateAlert)
	g.DELETE("/alerts/:alertId", h.DeleteAlert)
	g.POST("/alert-events/:eventId/read", h.MarkAlertEventRead)
	g.GET("/:id/activity", h.Activity)
	g.GET("/:id/stats", h.Stats)
	g.GET("/:id/positions", h.Positions)
	g.GET("/:id/exposure", h.Exposure)
	g.GET("/:id/performance", h.Performance)

	alerts := router.Group("/trader-alerts")
	if len(jwtMiddleware) > 0 {
		alerts.Use(jwtMiddleware[0])
	}
	alerts.PATCH("/:alertId", h.UpdateAlert)
	alerts.DELETE("/:alertId", h.DeleteAlert)
	alerts.GET("/unread-count", h.UnreadAlertCount)

	events := router.Group("/trader-alert-events")
	if len(jwtMiddleware) > 0 {
		events.Use(jwtMiddleware[0])
	}
	events.POST("/:eventId/read", h.MarkAlertEventRead)
}

// getUserID requires an authenticated caller (AUTH-005 anonymous, AUTH-003
// malformed — parity with the legacy groups API).
func (h *Handler) getUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, ok := c.Get("user_id")
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token identity"))
		return uuid.Nil, false
	}
	return id, true
}

func appErr(err error) *domain.AppError {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return domain.NewError(domain.ErrCodeInternal, "internal error").WithErr(err)
}

func groupID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid group ID"))
		return uuid.Nil, false
	}
	return id, true
}

// normalizeMember validates one member input (venue required, address
// lowercased EVM).
func normalizeMember(in MemberInput) (MemberInput, error) {
	venue := strings.ToLower(strings.TrimSpace(in.Venue))
	if venue == "" {
		return in, domain.NewError(domain.ErrCodeValidation, "member venue is required")
	}
	addr := strings.ToLower(strings.TrimSpace(in.WalletAddress))
	if !addrRe.MatchString(addr) {
		return in, domain.NewError(domain.ErrCodeValidation, "invalid member address")
	}
	in.Venue = venue
	in.WalletAddress = addr
	in.Alias = strings.TrimSpace(in.Alias)
	in.Note = strings.TrimSpace(in.Note)
	return in, nil
}

// Create godoc
// @Summary      Create trader group
// @Description  Creates a group owned by the caller; duplicate name is GROUP-002. Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      CreateGroupRequest  true  "Group to create"
// @Success      201  {object}  api.Response{data=Group}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      409  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups [post]
func (h *Handler) Create(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	if err := req.Validate(); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	g, err := h.store.Create(c.Request.Context(), userID,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Description))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: g})
}

// List godoc
// @Summary      List trader groups
// @Description  The caller's groups with member counts. Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=[]Group}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups [get]
func (h *Handler) List(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	page := 1
	pageSize := 20
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
			return
		}
		page = n
	}
	if raw := c.Query("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size"))
			return
		}
		pageSize = n
	}
	if page < 1 {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
		return
	}
	if pageSize < 1 || pageSize > 100 {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size"))
		return
	}
	params := ListGroupsParams{
		Search:   c.Query("search"),
		Status:   c.Query("status"),
		Period:   c.DefaultQuery("period", "30D"),
		Sort:     c.DefaultQuery("sort", "name"),
		Dir:      c.DefaultQuery("dir", "asc"),
		Page:     page,
		PageSize: pageSize,
	}
	if params.Search == "" {
		params.Search = c.Query("q")
	}
	switch params.Period {
	case trader.Period1D, trader.Period7D, trader.Period30D, trader.PeriodALL, "90D":
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period"))
		return
	}
	switch params.Sort {
	case "name", "account_value", "unrealized_pnl", "roi", "volume", "open_positions", "win_rate", "updated_at", "created_at":
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort"))
		return
	}
	if params.Dir != "asc" && params.Dir != "desc" {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir"))
		return
	}
	if params.Status != "" {
		switch params.Status {
		case "empty", "ready", "partial", "stale", "error":
		default:
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid status"))
			return
		}
	}
	if raw := c.Query("favorite"); raw != "" {
		v := strings.EqualFold(raw, "true")
		if raw != "true" && raw != "false" && raw != "1" && raw != "0" {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid favorite"))
			return
		}
		params.Favorite = &v
	}
	items, total, err := h.store.List(c.Request.Context(), userID, params)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if items == nil {
		items = []*GroupListItem{}
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = (int(total) + pageSize - 1) / pageSize
	}
	hasMore := page*pageSize < int(total)
	api.RespondList(c, items, &api.Meta{
		Page:       page,
		TotalPages: totalPages,
		Total:      int64(total),
		Limit:      pageSize,
		HasMore:    hasMore,
	})
}

// Get godoc
// @Summary      Get trader group
// @Description  One owned group with member count. Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Group ID"
// @Success      200  {object}  api.Response{data=Group}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	g, err := h.store.Get(c.Request.Context(), id, userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, g)
}

// Update godoc
// @Summary      Update trader group
// @Description  Renames/edits an owned group; owner immutable; rename collision is GROUP-002. Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string              true  "Group ID"
// @Param        request  body      UpdateGroupRequest  true  "Fields to update"
// @Success      200  {object}  api.Response{data=Group}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Failure      409  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id} [patch]
func (h *Handler) Update(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	name, desc, isFav, notifyEn, err := req.Validate()
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	g, err := h.store.Update(c.Request.Context(), id, userID, name, desc, isFav, notifyEn)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, g)
}

// Overview godoc
func (h *Handler) Overview(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "30D")
	stats, err := h.store.Overview(c.Request.Context(), userID, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, stats)
}

// Favorite godoc
func (h *Handler) Favorite(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req FavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	g, err := h.store.ToggleFavorite(c.Request.Context(), id, userID, req.IsFavorite)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, g)
}

// Duplicate godoc
func (h *Handler) Duplicate(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req DuplicateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = DuplicateGroupRequest{}
	}
	if err := req.Validate(); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	var name *string
	if strings.TrimSpace(req.Name) != "" {
		trimmed := strings.TrimSpace(req.Name)
		name = &trimmed
	}
	g, err := h.store.Duplicate(c.Request.Context(), id, userID, name)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: g})
}

// Delete godoc
// @Summary      Delete trader group
// @Description  Deletes an owned group; memberships cascade, registry rows stay. Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Group ID"
// @Success      204
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	if err := h.store.Delete(c.Request.Context(), id, userID); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.Status(http.StatusNoContent)
}

// AddMembers godoc
// @Summary      Add group members
// @Description  Adds (venue, address) memberships with optional alias/note; idempotent (existing rows keep alias/note). Unknown wallet is 404. Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string            true  "Group ID"
// @Param        request  body      AddMembersRequest  true  "Members to add"
// @Success      200  {object}  api.Response{data=map[string]int64}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/members [post]
func (h *Handler) AddMembers(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	items := make([]MemberInput, 0, len(req.Members))
	for _, in := range req.Members {
		norm, err := normalizeMember(in)
		if err != nil {
			api.RespondError(c, appErr(err))
			return
		}
		items = append(items, norm)
	}
	added, skipped, err := h.store.AddMembers(c.Request.Context(), id, userID, items)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, map[string]int64{"added": added, "skipped": skipped})
}

// ListMembers godoc
// @Summary      List group members
// @Description  Owned-group memberships with venue codes, display names and period metrics (null when a wallet has none for the period). Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string  true   "Group ID"
// @Param        period   query     string  false  "Metric window" enums(1D,7D,30D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=[]Member}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	page := 1
	pageSize := 20
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
			return
		}
		page = n
	}
	if raw := c.Query("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size"))
			return
		}
		pageSize = n
	}
	if page < 1 {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page"))
		return
	}
	if pageSize < 1 || pageSize > 100 {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size"))
		return
	}
	params := ListMembersParams{
		Period:   c.DefaultQuery("period", "30D"),
		Search:   c.Query("search"),
		Sort:     c.DefaultQuery("sort", "wallet_address"),
		Dir:      c.DefaultQuery("dir", "asc"),
		Page:     page,
		PageSize: pageSize,
		Filter:   c.DefaultQuery("filter", "all"),
	}
	switch params.Period {
	case trader.Period1D, trader.Period7D, trader.Period30D, trader.PeriodALL, "90D":
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period"))
		return
	}
	switch params.Sort {
	case "wallet_address", "account_value", "unrealized_pnl", "roi", "volume", "open_positions", "last_trade_at", "updated_at":
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort"))
		return
	}
	if params.Dir != "asc" && params.Dir != "desc" {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid dir"))
		return
	}
	switch params.Filter {
	case "all", "missing_data", "stale", "in_other_groups":
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid filter"))
		return
	}
	items, total, err := h.store.ListMembers(c.Request.Context(), id, userID, params)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if items == nil {
		items = []*MemberRow{}
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = (int(total) + pageSize - 1) / pageSize
	}
	hasMore := page*pageSize < int(total)
	api.RespondList(c, items, &api.Meta{
		Page:       page,
		TotalPages: totalPages,
		Total:      int64(total),
		Limit:      pageSize,
		HasMore:    hasMore,
	})
}

// RemoveMembers godoc
// @Summary      Remove group members
// @Description  Removes memberships (relationship only); absent rows are a no-op. Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string            true  "Group ID"
// @Param        request  body      AddMembersRequest  true  "Members to remove (venue + address used)"
// @Success      200  {object}  api.Response{data=map[string]int64}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/members [delete]
func (h *Handler) RemoveMembers(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	byVenue := map[string][]string{}
	for _, in := range req.Members {
		norm, err := normalizeMember(in)
		if err != nil {
			api.RespondError(c, appErr(err))
			return
		}
		byVenue[norm.Venue] = append(byVenue[norm.Venue], norm.WalletAddress)
	}
	var removed int64
	for venue, addrs := range byVenue {
		venueID, err := h.store.VenueIDByCode(c.Request.Context(), venue)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+venue))
			return
		}
		n, err := h.store.RemoveMembers(c.Request.Context(), id, userID, venueID, addrs)
		if err != nil {
			api.RespondError(c, appErr(err))
			return
		}
		removed += n
	}
	api.RespondSuccess(c, map[string]int64{"removed": removed})
}

// UpdateMembers godoc
// @Summary      Update group members
// @Description  Sets alias/note on existing memberships. Per item: absent field keeps, present value sets (trimmed), present empty clears to NULL. Alias max 100 runes, note max 500. Absent memberships are a no-op. Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string                true  "Group ID"
// @Param        request  body      UpdateMembersRequest  true  "Members to update"
// @Success      200  {object}  api.Response{data=map[string]int64}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/members [patch]
func (h *Handler) UpdateMembers(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req UpdateMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	items := make([]ValidatedMemberUpdate, 0, len(req.Members))
	for _, in := range req.Members {
		validated, err := in.Validate()
		if err != nil {
			api.RespondError(c, appErr(err))
			return
		}
		items = append(items, validated)
	}
	updated, err := h.store.UpdateMembers(c.Request.Context(), id, userID, items)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, map[string]int64{"updated": updated})
}
