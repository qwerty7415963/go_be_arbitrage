package tradergroup

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// Store is the group persistence surface (*Repository implements it; mocked
// in handler tests).
type Store interface {
	Create(ctx context.Context, userID uuid.UUID, name, desc string) (*Group, error)
	List(ctx context.Context, userID uuid.UUID) ([]*Group, error)
	Get(ctx context.Context, groupID, userID uuid.UUID) (*Group, error)
	Update(ctx context.Context, groupID, userID uuid.UUID, name, desc *string) (*Group, error)
	Delete(ctx context.Context, groupID, userID uuid.UUID) error
	AddMembers(ctx context.Context, groupID, userID uuid.UUID, items []MemberInput) (int64, error)
	RemoveMembers(ctx context.Context, groupID, userID, venueID uuid.UUID, addrs []string) (int64, error)
	UpdateMembers(ctx context.Context, groupID, userID uuid.UUID, items []ValidatedMemberUpdate) (int64, error)
	ListMembers(ctx context.Context, groupID, userID uuid.UUID, period string) ([]*Member, error)
	OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error)
	VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error)
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
	g.GET("/:id", h.Get)
	g.PATCH("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	g.POST("/:id/members", h.AddMembers)
	g.GET("/:id/members", h.ListMembers)
	g.PATCH("/:id/members", h.UpdateMembers)
	g.DELETE("/:id/members", h.RemoveMembers)
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
// @Router       /api/v1/trader-groups [post]
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
// @Router       /api/v1/trader-groups [get]
func (h *Handler) List(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	groups, err := h.store.List(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, groups)
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
// @Router       /api/v1/trader-groups/{id} [get]
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
// @Router       /api/v1/trader-groups/{id} [patch]
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
	name, desc, err := req.Validate()
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	g, err := h.store.Update(c.Request.Context(), id, userID, name, desc)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, g)
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
// @Router       /api/v1/trader-groups/{id} [delete]
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
// @Router       /api/v1/trader-groups/{id}/members [post]
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
	added, err := h.store.AddMembers(c.Request.Context(), id, userID, items)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, map[string]int64{"added": added})
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
// @Router       /api/v1/trader-groups/{id}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	members, err := h.store.ListMembers(c.Request.Context(), id, userID, c.DefaultQuery("period", "30D"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if members == nil {
		members = []*Member{}
	}
	api.RespondSuccess(c, members)
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
// @Router       /api/v1/trader-groups/{id}/members [delete]
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
// @Router       /api/v1/trader-groups/{id}/members [patch]
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
