package storage

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Handler struct {
	opportunityRepo *OpportunityRepository
	decisionRepo    *DecisionRepository
	auditRepo       *AuditRepository
	replayReader    *ReplayReader
	retentionSvc    *RetentionService
}

func NewHandler(
	opportunityRepo *OpportunityRepository,
	decisionRepo *DecisionRepository,
	auditRepo *AuditRepository,
	replayReader *ReplayReader,
	retentionSvc *RetentionService,
) *Handler {
	return &Handler{
		opportunityRepo: opportunityRepo,
		decisionRepo:    decisionRepo,
		auditRepo:       auditRepo,
		replayReader:    replayReader,
		retentionSvc:    retentionSvc,
	}
}

// ListOpportunities godoc
// @Summary      List opportunities
// @Description  List arbitrage opportunities with pagination
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        tenant_id  query    string true   "Tenant ID"
// @Param        limit      query    int    false  "Page size"  default(20)
// @Param        offset     query    int    false  "Offset"     default(0)
// @Success      200  {object}  api.Response
// @Failure      400  {object}  api.Response
// @Router       /api/v1/storage/opportunities [get]
func (h *Handler) ListOpportunities(c *gin.Context) {
	tenantIDStr := c.Query("tenant_id")
	if tenantIDStr == "" {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "tenant_id is required"))
		return
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid tenant_id"))
		return
	}

	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	opportunities, err := h.opportunityRepo.ListByTenant(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list opportunities", err))
		return
	}

	api.RespondList(c, opportunities, &api.Meta{
		Cursor:  strconv.Itoa(offset + limit),
		HasMore: len(opportunities) == limit,
		Limit:   limit,
	})
}

// GetOpportunity godoc
// @Summary      Get opportunity by ID
// @Description  Get a single opportunity with its legs
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Opportunity ID"
// @Success      200  {object}  api.Response
// @Failure      404  {object}  api.Response
// @Router       /api/v1/storage/opportunities/{id} [get]
func (h *Handler) GetOpportunity(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid opportunity ID"))
		return
	}

	opp, err := h.opportunityRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "opportunity not found"))
		return
	}

	legs, err := h.opportunityRepo.ListLegsByOpportunity(c.Request.Context(), id)
	if err == nil {
		opp.Legs = legs
	}

	api.RespondSuccess(c, opp)
}

// ListStrategyDecisions godoc
// @Summary      List strategy decisions
// @Description  List strategy decisions for a strategy instance
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        instance_id  path      string  true   "Strategy Instance ID"
// @Param        limit        query     int     false  "Page size"  default(20)
// @Param        offset       query     int     false  "Offset"     default(0)
// @Success      200  {object}  api.Response
// @Failure      500  {object}  api.Response
// @Router       /api/v1/storage/decisions/strategy/{instance_id} [get]
func (h *Handler) ListStrategyDecisions(c *gin.Context) {
	instanceID, err := uuid.Parse(c.Param("instance_id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid instance ID"))
		return
	}

	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	decisions, err := h.decisionRepo.ListStrategyDecisionsByInstance(c.Request.Context(), instanceID, limit, offset)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list strategy decisions", err))
		return
	}

	api.RespondSuccess(c, decisions)
}

// ListRiskDecisions godoc
// @Summary      List risk decisions
// @Description  List risk decisions for a strategy instance
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        instance_id  path      string  true   "Strategy Instance ID"
// @Param        limit        query     int     false  "Page size"  default(20)
// @Param        offset       query     int     false  "Offset"     default(0)
// @Success      200  {object}  api.Response
// @Failure      500  {object}  api.Response
// @Router       /api/v1/storage/decisions/risk/{instance_id} [get]
func (h *Handler) ListRiskDecisions(c *gin.Context) {
	instanceID, err := uuid.Parse(c.Param("instance_id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid instance ID"))
		return
	}

	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	decisions, err := h.decisionRepo.ListRiskDecisionsByInstance(c.Request.Context(), instanceID, limit, offset)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list risk decisions", err))
		return
	}

	api.RespondSuccess(c, decisions)
}

// ListAuditEvents godoc
// @Summary      List audit events
// @Description  List audit events for a tenant
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        tenant_id  path      string  true   "Tenant ID"
// @Param        limit      query     int     false  "Page size"  default(20)
// @Param        offset     query     int     false  "Offset"     default(0)
// @Success      200  {object}  api.Response
// @Failure      500  {object}  api.Response
// @Router       /api/v1/storage/audit/{tenant_id} [get]
func (h *Handler) ListAuditEvents(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenant_id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid tenant ID"))
		return
	}

	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	events, err := h.auditRepo.ListAuditEventsByTenant(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list audit events", err))
		return
	}

	api.RespondSuccess(c, events)
}

// ReplayMarketEvents godoc
// @Summary      Replay market events
// @Description  Replay market events from a time range
// @Tags         storage
// @Accept       json
// @Produce      json
// @Param        start_time  query     string  true   "Start time (RFC3339)"
// @Param        end_time    query     string  true   "End time (RFC3339)"
// @Param        venue_id    query     string  false  "Venue ID"
// @Param        instrument_id  query   string  false  "Instrument ID"
// @Param        limit       query     int     false  "Max events"  default(100)
// @Success      200  {object}  api.Response
// @Failure      400  {object}  api.Response
// @Router       /api/v1/storage/replay/market [get]
func (h *Handler) ReplayMarketEvents(c *gin.Context) {
	startTime, err := time.Parse(time.RFC3339, c.Query("start_time"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid start_time format"))
		return
	}

	endTime, err := time.Parse(time.RFC3339, c.Query("end_time"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid end_time format"))
		return
	}

	var venueID, instrumentID *uuid.UUID
	if vid := c.Query("venue_id"); vid != "" {
		id, _ := uuid.Parse(vid)
		venueID = &id
	}
	if iid := c.Query("instrument_id"); iid != "" {
		id, _ := uuid.Parse(iid)
		instrumentID = &id
	}

	limit := 100
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}

	sequence, err := h.replayReader.ReadMarketEvents(c.Request.Context(), startTime, endTime, venueID, instrumentID, limit)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to replay market events", err))
		return
	}

	api.RespondSuccess(c, sequence)
}

// CleanupData godoc
// @Summary      Cleanup old data
// @Description  Run data retention cleanup with default config
// @Tags         storage
// @Accept       json
// @Produce      json
// @Success      200  {object}  api.Response
// @Failure      500  {object}  api.Response
// @Router       /api/v1/storage/retention/cleanup [post]
func (h *Handler) CleanupData(c *gin.Context) {
	config := DefaultRetentionConfig()
	result, err := h.retentionSvc.RunFullCleanup(c.Request.Context(), config)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "retention cleanup failed", err))
		return
	}

	api.RespondSuccess(c, result)
}
