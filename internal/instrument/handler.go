package instrument

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

// Create godoc
// @Summary      Create instrument
// @Description  Create a new instrument
// @Tags         instruments
// @Accept       json
// @Produce      json
// @Param        request  body      CreateInstrumentRequest  true  "Instrument to create"
// @Success      201      {object}  api.Response{data=Instrument}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      409      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments [post]
func (h *Handler) Create(c *gin.Context) {
	var req CreateInstrumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondValidationError(c, err.Error())
		return
	}

	inst, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, api.Response{
		Success: true,
		Data:    inst,
	})
}

// GetByID godoc
// @Summary      Get instrument
// @Description  Get instrument by ID
// @Tags         instruments
// @Produce      json
// @Param        id   path      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=Instrument}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondValidationError(c, "invalid instrument ID")
		return
	}

	inst, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    inst,
	})
}

// List godoc
// @Summary      List instruments
// @Description  Get all instruments
// @Tags         instruments
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Instrument}
// @Failure      500  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments [get]
func (h *Handler) List(c *gin.Context) {
	instruments, err := h.service.List(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    instruments,
	})
}

// ListTradable godoc
// @Summary      List tradable instruments
// @Description  Get all instruments with trading enabled
// @Tags         instruments
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Instrument}
// @Failure      500  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments/tradable [get]
func (h *Handler) ListTradable(c *gin.Context) {
	instruments, err := h.service.ListTradable(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    instruments,
	})
}

// Update godoc
// @Summary      Update instrument
// @Description  Update instrument by ID
// @Tags         instruments
// @Accept       json
// @Produce      json
// @Param        id       path      string                 true  "Instrument ID"
// @Param        request  body      UpdateInstrumentRequest  true  "Fields to update"
// @Success      200      {object}  api.Response{data=Instrument}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondValidationError(c, "invalid instrument ID")
		return
	}

	var req UpdateInstrumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondValidationError(c, err.Error())
		return
	}

	inst, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    inst,
	})
}

// EnableTrading godoc
// @Summary      Enable/disable trading
// @Description  Enable or disable trading for an instrument
// @Tags         instruments
// @Accept       json
// @Produce      json
// @Param        id       path      string                  true  "Instrument ID"
// @Param        request  body      EnableTradingRequest  true  "Enable/disable trading"
// @Success      200      {object}  api.Response
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments/{id}/trading [put]
func (h *Handler) EnableTrading(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondValidationError(c, "invalid instrument ID")
		return
	}

	var req EnableTradingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondValidationError(c, err.Error())
		return
	}

	if err := h.service.EnableTrading(c.Request.Context(), id, req.Enabled); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
	})
}

// Delete godoc
// @Summary      Delete instrument
// @Description  Delete instrument by ID
// @Tags         instruments
// @Produce      json
// @Param        id   path      string  true  "Instrument ID"
// @Success      204
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/instruments/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondValidationError(c, "invalid instrument ID")
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// CreateVenueInstrument godoc
// @Summary      Create venue instrument mapping
// @Description  Map a venue symbol to a canonical instrument
// @Tags         instruments
// @Accept       json
// @Produce      json
// @Param        request  body      CreateVenueInstrumentRequest  true  "Venue instrument mapping"
// @Success      201      {object}  api.Response{data=VenueInstrument}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      409      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venue-instruments [post]
func (h *Handler) CreateVenueInstrument(c *gin.Context) {
	var req CreateVenueInstrumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondValidationError(c, err.Error())
		return
	}

	vi, err := h.service.CreateVenueInstrument(c.Request.Context(), &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, api.Response{
		Success: true,
		Data:    vi,
	})
}

// ListVenueInstruments godoc
// @Summary      List venue instruments
// @Description  Get all venue instruments for a venue
// @Tags         instruments
// @Produce      json
// @Param        venue_id  path      string  true  "Venue ID"
// @Success      200       {object}  api.Response{data=[]VenueInstrument}
// @Failure      400       {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venue-instruments [get]
func (h *Handler) ListVenueInstruments(c *gin.Context) {
	venueID, err := uuid.Parse(c.Query("venue_id"))
	if err != nil {
		respondValidationError(c, "invalid venue_id")
		return
	}

	vi, err := h.service.ListVenueInstruments(c.Request.Context(), venueID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    vi,
	})
}

func respondValidationError(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, api.Response{
		Success: false,
		Error: &api.ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: message,
		},
	})
}

func respondError(c *gin.Context, err error) {
	appErr, ok := err.(*domain.AppError)
	if !ok {
		appErr = domain.WrapError(domain.ErrCodeConfigInvalid, err.Error(), err)
	}

	c.JSON(appErr.StatusCode, api.Response{
		Success: false,
		Error: &api.ErrorBody{
			Code:    string(appErr.Code),
			Message: appErr.Message,
			Details: appErr.Details,
		},
	})
}
