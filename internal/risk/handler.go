package risk

import (
	"net/http"

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
	risk := router.Group("/risk")
	if len(jwtMiddleware) > 0 {
		risk.Use(jwtMiddleware[0])
	}
	risk.Use(func(c *gin.Context) {
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
		risk.GET("/policies", h.ListPolicies)
		risk.POST("/policies", h.CreatePolicy)
		risk.GET("/policies/:id", h.GetPolicy)
		risk.PUT("/policies/:id", h.UpdatePolicy)
		risk.DELETE("/policies/:id", h.DeletePolicy)
		risk.POST("/kill-switch/enable", h.EnableKillSwitch)
		risk.POST("/kill-switch/disable", h.DisableKillSwitch)
		risk.GET("/kill-switch", h.GetKillSwitch)
		risk.POST("/pre-trade-check", h.PreTradeCheck)
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

func (h *Handler) ListPolicies(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	policies, err := h.service.ListPolicies(c.Request.Context(), tenantID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to list policies"))
		return
	}
	if policies == nil {
		policies = []*RiskPolicy{}
	}
	api.RespondList(c, policies, nil)
}

func (h *Handler) CreatePolicy(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req CreatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	policy, err := h.service.CreatePolicy(c.Request.Context(), &req, tenantID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to create policy"))
		return
	}
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: policy})
}

func (h *Handler) GetPolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid policy ID"))
		return
	}
	policy, err := h.service.GetPolicy(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "policy not found"))
		return
	}
	api.RespondSuccess(c, policy)
}

func (h *Handler) UpdatePolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid policy ID"))
		return
	}
	var req UpdatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	policy, err := h.service.UpdatePolicy(c.Request.Context(), id, &req)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to update policy"))
		return
	}
	api.RespondSuccess(c, policy)
}

func (h *Handler) DeletePolicy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid policy ID"))
		return
	}
	if err := h.service.DeletePolicy(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "failed to delete policy"))
		return
	}
	api.RespondSuccess(c, gin.H{"message": "policy deleted"})
}

func (h *Handler) EnableKillSwitch(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	h.service.EnableKillSwitch(req.Reason)
	api.RespondSuccess(c, gin.H{"message": "kill switch enabled"})
}

func (h *Handler) DisableKillSwitch(c *gin.Context) {
	h.service.DisableKillSwitch()
	api.RespondSuccess(c, gin.H{"message": "kill switch disabled"})
}

func (h *Handler) GetKillSwitch(c *gin.Context) {
	api.RespondSuccess(c, h.service.GetKillSwitch())
}

func (h *Handler) PreTradeCheck(c *gin.Context) {
	var req PreTradeCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	result := h.service.PreTradeCheck(c.Request.Context(), &req, 100000, 0, 1, 50000, true, true)
	api.RespondSuccess(c, result)
}
