package walletgroup

import (
	"errors"
	"net/http"
	"strconv"
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
// @Description  Create a wallet group owned by the current user
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
// @Description  Update name/description/color of an owned group
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
// @Success      200      {object}  api.Response{data=map[string]int64}
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

	c.JSON(http.StatusOK, api.Response{Success: true, Data: map[string]int64{"added": created}})
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

	c.JSON(http.StatusNoContent, nil)
}

// ListWallets godoc
// @Summary      List group wallets
// @Description  Paginated wallets of a group with partial address search
// @Tags         groups
// @Produce      json
// @Param        id      path   string  true   "Group ID"
// @Param        search  query  string  false  "Partial address match"
// @Param        page    query  int     false  "Page (default 1)"
// @Param        limit   query  int     false  "Page size (default 50, max 200)"
// @Success      200     {object}  api.Response{data=[]WalletRef,meta=api.Meta}
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
