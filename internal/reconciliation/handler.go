package reconciliation

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
	recon := router.Group("/reconciliation")
	recon.Use(middleware.RequireRole("admin"))
	{
		recon.POST("/runs", h.CreateRun)
		recon.GET("/runs", h.ListRuns)
		recon.GET("/runs/:id", h.GetRun)
		recon.GET("/runs/:id/items", h.ListItems)
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

func (h *Handler) CreateRun(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req CreateRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	run, err := h.service.StartReconciliation(c.Request.Context(), tenantID, req.VenueAccountID, req.TriggerSource)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, api.Response{Success: true, Data: run})
}

func (h *Handler) GetRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid run ID"))
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "reconciliation run not found"))
		return
	}
	api.RespondSuccess(c, run)
}

func (h *Handler) ListRuns(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	runs, err := h.service.ListRuns(c.Request.Context(), tenantID, 50, 0)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	if runs == nil {
		runs = []*ReconciliationRun{}
	}
	api.RespondList(c, runs, nil)
}

func (h *Handler) ListItems(c *gin.Context) {
	runID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid run ID"))
		return
	}
	items, err := h.service.ListItems(c.Request.Context(), runID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, err.Error()))
		return
	}
	if items == nil {
		items = []*ReconciliationItem{}
	}
	api.RespondList(c, items, nil)
}
