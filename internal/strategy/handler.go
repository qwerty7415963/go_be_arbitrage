package strategy

import (
	"net/http"
	"strconv"

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
	strategies := router.Group("/strategies")
	if len(jwtMiddleware) > 0 {
		strategies.Use(jwtMiddleware[0])
	}
	strategies.Use(func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "role not found in token"))
			c.Abort()
			return
		}
		roleStr, ok := role.(string)
		if !ok {
			api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid role type"))
			c.Abort()
			return
		}
		if roleStr != "admin" {
			api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "insufficient permissions"))
			c.Abort()
			return
		}
		c.Next()
	})
	{
		strategies.GET("", h.List)
		strategies.POST("", h.Create)
		strategies.GET("/:id", h.GetByID)
		strategies.PUT("/:id", h.Update)
		strategies.DELETE("/:id", h.Delete)
		strategies.POST("/:id/start", h.Start)
		strategies.POST("/:id/stop", h.Stop)
		strategies.GET("/:id/decisions", h.ListDecisions)
	}
}

func (h *Handler) getTenantID(c *gin.Context) (uuid.UUID, bool) {
	tenantIDStr, exists := c.Get("tenant_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "tenant not found"))
		return uuid.Nil, false
	}

	s, ok := tenantIDStr.(string)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid tenant"))
		return uuid.Nil, false
	}

	tenantID, err := uuid.Parse(s)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid tenant"))
		return uuid.Nil, false
	}

	return tenantID, true
}

// List godoc
// @Summary      List strategy instances
// @Description  Strategy instances for the caller's tenant. Requires JWT + admin role (group middleware: 401 AUTH-003 without token, 403 AUTH-005 for non-admin).
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=[]StrategyInstance}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003 no/invalid token"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005 not admin"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies [get]
func (h *Handler) List(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}

	instances, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to list strategies"))
		return
	}

	if instances == nil {
		instances = []*StrategyInstance{}
	}

	api.RespondList(c, instances, nil)
}

// Create godoc
// @Summary      Create strategy instance
// @Description  Creates a strategy instance scoped to the caller's tenant. Requires JWT + admin role.
// @Tags         strategies
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      CreateStrategyRequest  true  "Strategy definition"
// @Success      201  {object}  api.Response{data=StrategyInstance}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid body"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies [post]
func (h *Handler) Create(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}

	var req CreateStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}

	instance, err := h.service.Create(c.Request.Context(), &req, tenantID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to create strategy"))
		return
	}

	c.JSON(http.StatusCreated, api.Response{
		Success: true,
		Data:    instance,
	})
}

// GetByID godoc
// @Summary      Get strategy instance
// @Description  One strategy instance by ID. Requires JWT + admin role.
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Strategy instance ID"
// @Success      200  {object}  api.Response{data=StrategyInstance}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid ID"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "COMMON-903"
// @Router       /api/v1/strategies/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	instance, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "strategy not found"))
		return
	}

	api.RespondSuccess(c, instance)
}

// Update godoc
// @Summary      Update strategy instance
// @Description  Updates a strategy instance by ID. Requires JWT + admin role.
// @Tags         strategies
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string                  true  "Strategy instance ID"
// @Param        request  body      UpdateStrategyRequest   true  "Fields to update"
// @Success      200  {object}  api.Response{data=StrategyInstance}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	var req UpdateStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}

	instance, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to update strategy"))
		return
	}

	api.RespondSuccess(c, instance)
}

// Delete godoc
// @Summary      Delete strategy instance
// @Description  Deletes a strategy instance by ID. Requires JWT + admin role.
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Strategy instance ID"
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to delete strategy"))
		return
	}

	api.RespondSuccess(c, gin.H{"message": "strategy deleted"})
}

// Start godoc
// @Summary      Start strategy instance
// @Description  Starts an existing strategy instance. Requires JWT + admin role.
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Strategy instance ID"
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies/{id}/start [post]
func (h *Handler) Start(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	if err := h.service.Start(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}

	api.RespondSuccess(c, gin.H{"message": "strategy started"})
}

// Stop godoc
// @Summary      Stop strategy instance
// @Description  Stops a running strategy instance. Requires JWT + admin role.
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Strategy instance ID"
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies/{id}/stop [post]
func (h *Handler) Stop(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	if err := h.service.Stop(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}

	api.RespondSuccess(c, gin.H{"message": "strategy stopped"})
}

// ListDecisions godoc
// @Summary      List strategy decisions
// @Description  Recent decisions for one strategy instance. Requires JWT + admin role.
// @Tags         strategies
// @Produce      json
// @Security     BearerAuth
// @Param        id     path    string  true   "Strategy instance ID"
// @Param        limit  query   int     false  "Max rows" minimum(1) default(50)
// @Success      200  {object}  api.Response{data=[]StrategyDecision}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/strategies/{id}/decisions [get]
func (h *Handler) ListDecisions(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid strategy ID"))
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	decisions, err := h.service.GetDecisions(c.Request.Context(), id, limit)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to list decisions"))
		return
	}

	if decisions == nil {
		decisions = []*StrategyDecision{}
	}

	api.RespondList(c, decisions, nil)
}
