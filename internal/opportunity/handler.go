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

func (h *Handler) ListOpportunities(c *gin.Context) {
	opps := h.service.GetAllOpportunities()

	if opps == nil {
		opps = []*Opportunity{}
	}

	api.RespondList(c, opps, nil)
}

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

func (h *Handler) TriggerScan(c *gin.Context) {
	result := h.service.ScanNow()
	api.RespondSuccess(c, result)
}

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
