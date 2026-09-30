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

// ListPolicies godoc
// @Summary      List risk policies
// @Description  Risk policies for the caller's tenant. Requires JWT + admin role (401 AUTH-003 without token, 403 AUTH-005 for non-admin).
// @Tags         risk
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=[]RiskPolicy}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/risk/policies [get]
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

// CreatePolicy godoc
// @Summary      Create risk policy
// @Description  Creates a risk policy scoped to the caller's tenant. Requires JWT + admin role.
// @Tags         risk
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      CreatePolicyRequest  true  "Policy definition"
// @Success      201  {object}  api.Response{data=RiskPolicy}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/risk/policies [post]
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

// GetPolicy godoc
// @Summary      Get risk policy
// @Description  One risk policy by ID. Requires JWT + admin role.
// @Tags         risk
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Policy ID"
// @Success      200  {object}  api.Response{data=RiskPolicy}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "COMMON-903"
// @Router       /api/v1/risk/policies/{id} [get]
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

// UpdatePolicy godoc
// @Summary      Update risk policy
// @Description  Updates a risk policy by ID. Requires JWT + admin role.
// @Tags         risk
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string                true  "Policy ID"
// @Param        request  body      UpdatePolicyRequest   true  "Fields to update"
// @Success      200  {object}  api.Response{data=RiskPolicy}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/risk/policies/{id} [put]
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

// DeletePolicy godoc
// @Summary      Delete risk policy
// @Description  Deletes a risk policy by ID. Requires JWT + admin role.
// @Tags         risk
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Policy ID"
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/risk/policies/{id} [delete]
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

// EnableKillSwitch godoc
// @Summary      Enable kill switch
// @Description  Trips the global kill switch (blocks new risk) with an optional reason. Requires JWT + admin role.
// @Tags         risk
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Router       /api/v1/risk/kill-switch/enable [post]
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

// DisableKillSwitch godoc
// @Summary      Disable kill switch
// @Description  Resets the global kill switch. Requires JWT + admin role.
// @Tags         risk
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Router       /api/v1/risk/kill-switch/disable [post]
func (h *Handler) DisableKillSwitch(c *gin.Context) {
	h.service.DisableKillSwitch()
	api.RespondSuccess(c, gin.H{"message": "kill switch disabled"})
}

// GetKillSwitch godoc
// @Summary      Get kill switch state
// @Description  Current kill switch status (enabled flag, reason, timestamp). Requires JWT + admin role.
// @Tags         risk
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=KillSwitch}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Router       /api/v1/risk/kill-switch [get]
func (h *Handler) GetKillSwitch(c *gin.Context) {
	api.RespondSuccess(c, h.service.GetKillSwitch())
}

// PreTradeCheck godoc
// @Summary      Pre-trade risk check
// @Description  Evaluates a hypothetical order against risk policies and returns allow/reject with reasons. Requires JWT + admin role.
// @Tags         risk
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      PreTradeCheckRequest  true  "Order to check"
// @Success      200  {object}  api.Response{data=PreTradeCheckResult}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Router       /api/v1/risk/pre-trade-check [post]
func (h *Handler) PreTradeCheck(c *gin.Context) {
	var req PreTradeCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	result := h.service.PreTradeCheck(c.Request.Context(), &req, 100000, 0, 1, 50000, true, true)
	api.RespondSuccess(c, result)
}
