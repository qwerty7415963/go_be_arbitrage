package execution

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	executions := router.Group("/executions")
	executions.Use(middleware.RequireRole("admin"))
	{
		executions.GET("", h.List)
		executions.POST("", h.Create)
		executions.GET("/:id", h.GetByID)
		executions.POST("/:id/submit", h.Submit)
		executions.POST("/:id/cancel", h.Cancel)
		executions.GET("/:id/legs", h.ListLegs)
		executions.GET("/:id/fills", h.ListFills)
	}
}

func (h *Handler) getTenantID(c *gin.Context) (uuid.UUID, bool) {
	tenantIDStr, exists := c.Get("tenant_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "tenant not found"))
		return uuid.Nil, false
	}
	tenantID, ok := tenantIDStr.(uuid.UUID)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid tenant"))
		return uuid.Nil, false
	}
	return tenantID, true
}

func (h *Handler) List(c *gin.Context) {
	api.RespondList(c, []*Execution{}, nil)
}

func (h *Handler) Create(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req CreateExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	exec, err := h.service.CreateExecution(c.Request.Context(), tenantID, &req)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: exec})
}

func (h *Handler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid execution ID"))
		return
	}
	exec, err := h.service.GetExecution(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "execution not found"))
		return
	}
	api.RespondSuccess(c, exec)
}

func (h *Handler) Submit(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid execution ID"))
		return
	}
	if err := h.service.SubmitExecution(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	api.RespondSuccess(c, gin.H{"message": "execution submitted"})
}

func (h *Handler) Cancel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid execution ID"))
		return
	}
	if err := h.service.CancelExecution(c.Request.Context(), id); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	api.RespondSuccess(c, gin.H{"message": "execution canceled"})
}

func (h *Handler) ListLegs(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid execution ID"))
		return
	}
	legs, err := h.service.ListLegs(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	if legs == nil {
		legs = []*ExecutionLeg{}
	}
	api.RespondList(c, legs, nil)
}

func (h *Handler) ListFills(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid execution ID"))
		return
	}
	fills, err := h.service.ListFills(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	if fills == nil {
		fills = []*Fill{}
	}
	api.RespondList(c, fills, nil)
}
