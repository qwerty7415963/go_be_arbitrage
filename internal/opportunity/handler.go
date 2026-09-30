package opportunity

import (
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

func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	opportunities := router.Group("/opportunities")
	{
		opportunities.GET("", h.ListOpportunities)
		opportunities.GET("/:id", h.GetOpportunity)
		opportunities.GET("/type/:type", h.ListByType)
		opportunities.GET("/instrument/:instrument_id", h.ListByInstrument)
		opportunities.POST("/scan", h.TriggerScan)
		opportunities.DELETE("/:id", h.RemoveOpportunity)
	}
}

// ListOpportunities godoc
// @Summary      List opportunities (public)
// @Description  All in-memory opportunities from the live scanner, newest first; empty array when none. Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Opportunity}
// @Router       /api/v1/opportunities [get]
func (h *Handler) ListOpportunities(c *gin.Context) {
	opps := h.service.GetAllOpportunities()

	if opps == nil {
		opps = []*Opportunity{}
	}

	api.RespondList(c, opps, nil)
}

// GetOpportunity godoc
// @Summary      Get opportunity (public)
// @Description  One opportunity by ID. Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Param        id   path      string  true  "Opportunity ID"
// @Success      200  {object}  api.Response{data=Opportunity}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid ID"
// @Failure      404  {object}  api.Response{error=api.ErrorBody}  "COMMON-903 not found"
// @Router       /api/v1/opportunities/{id} [get]
func (h *Handler) GetOpportunity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid opportunity ID"))
		return
	}

	opp := h.service.GetOpportunity(id)
	if opp == nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "opportunity not found"))
		return
	}

	api.RespondSuccess(c, opp)
}

// ListByType godoc
// @Summary      List opportunities by type (public)
// @Description  Opportunities filtered by arbitrage type. Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Param        type   path      string  true   "Opportunity type" Enums(PRICE_ARBITRAGE,FUNDING_ARBITRAGE,BASIS_ARBITRAGE)
// @Success      200  {object}  api.Response{data=[]Opportunity}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid type"
// @Router       /api/v1/opportunities/type/{type} [get]
func (h *Handler) ListByType(c *gin.Context) {
	oppType := c.Param("type")
	switch OpportunityType(oppType) {
	case OpportunityTypePriceArb, OpportunityTypeFundingArb, OpportunityTypeBasisArb:
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid opportunity type"))
		return
	}

	opps := h.service.GetOpportunitiesByType(OpportunityType(oppType))
	if opps == nil {
		opps = []*Opportunity{}
	}

	api.RespondList(c, opps, nil)
}

// ListByInstrument godoc
// @Summary      List opportunities by instrument (public)
// @Description  Opportunities for one instrument ID. Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Param        instrument_id   path      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=[]Opportunity}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid ID"
// @Router       /api/v1/opportunities/instrument/{instrument_id} [get]
func (h *Handler) ListByInstrument(c *gin.Context) {
	instrumentIDStr := c.Param("instrument_id")
	instrumentID, err := uuid.Parse(instrumentIDStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid instrument ID"))
		return
	}

	opps := h.service.GetOpportunitiesByInstrument(instrumentID)
	if opps == nil {
		opps = []*Opportunity{}
	}

	api.RespondList(c, opps, nil)
}

// TriggerScan godoc
// @Summary      Trigger an immediate scan (public)
// @Description  Runs the opportunity scanner synchronously and returns what it found (count, venues, duration). Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Success      200  {object}  api.Response{data=ScanResult}
// @Router       /api/v1/opportunities/scan [post]
func (h *Handler) TriggerScan(c *gin.Context) {
	result := h.service.ScanNow()
	api.RespondSuccess(c, result)
}

// RemoveOpportunity godoc
// @Summary      Remove opportunity (public)
// @Description  Drops one opportunity from the in-memory store; idempotent. Public endpoint — no auth.
// @Tags         opportunities
// @Produce      json
// @Param        id   path      string  true  "Opportunity ID"
// @Success      200  {object}  api.Response{data=map[string]string}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "COMMON-902 invalid ID"
// @Router       /api/v1/opportunities/{id} [delete]
func (h *Handler) RemoveOpportunity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid opportunity ID"))
		return
	}

	h.service.RemoveOpportunity(id)
	api.RespondSuccess(c, gin.H{"message": "opportunity removed"})
}
