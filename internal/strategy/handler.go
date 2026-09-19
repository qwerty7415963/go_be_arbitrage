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
