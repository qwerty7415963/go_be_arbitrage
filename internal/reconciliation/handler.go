package reconciliation

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type ServiceInterface interface {
	StartReconciliation(ctx context.Context, tenantID, venueAccountID uuid.UUID, trigger TriggerSource) (*ReconciliationRun, error)
	GetRun(ctx context.Context, id uuid.UUID) (*ReconciliationRun, error)
	ListRuns(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error)
	ListItems(ctx context.Context, runID uuid.UUID) ([]*ReconciliationItem, error)
}

type Handler struct {
	service ServiceInterface
}

func NewHandler(service ServiceInterface) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	recon := router.Group("/reconciliation")
	if len(jwtMiddleware) > 0 {
		recon.Use(jwtMiddleware[0])
	}
	recon.Use(func(c *gin.Context) {
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

// CreateRun godoc
// @Summary      Start reconciliation run
// @Description  Starts a reconciliation run for a venue account in the caller's tenant. Requires JWT + admin role (401 AUTH-003 without token, 403 AUTH-005 for non-admin).
// @Tags         reconciliation
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      CreateRunRequest  true  "Run trigger"
// @Success      201  {object}  api.Response{data=ReconciliationRun}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/reconciliation/runs [post]
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

// GetRun godoc
// @Summary      Get reconciliation run
// @Description  One reconciliation run by ID. Requires JWT + admin role.
// @Tags         reconciliation
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Run ID"
// @Success      200  {object}  api.Response{data=ReconciliationRun}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "COMMON-903"
// @Router       /api/v1/reconciliation/runs/{id} [get]
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

// ListRuns godoc
// @Summary      List reconciliation runs
// @Description  Latest reconciliation runs for the caller's tenant (max 50). Requires JWT + admin role.
// @Tags         reconciliation
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=[]ReconciliationRun}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/reconciliation/runs [get]
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

// ListItems godoc
// @Summary      List reconciliation items
// @Description  Discrepancy items for one run; empty array when none. Requires JWT + admin role.
// @Tags         reconciliation
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Run ID"
// @Success      200  {object}  api.Response{data=[]ReconciliationItem}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}  "AUTH-003"
// @Failure      403  {object}  api.Response{error=api.ErrorBody}  "AUTH-005"
// @Failure      500  {object}  api.Response{error=api.ErrorBody}  "COMMON-901"
// @Router       /api/v1/reconciliation/runs/{id}/items [get]
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
