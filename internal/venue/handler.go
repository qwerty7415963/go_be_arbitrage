package venue

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
// @Summary      Create venue
// @Description  Create a new venue
// @Tags         venues
// @Accept       json
// @Produce      json
// @Param        request  body      CreateVenueRequest  true  "Venue to create"
// @Success      201      {object}  api.Response{data=Venue}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      409      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venues [post]
func (h *Handler) Create(c *gin.Context) {
	var req CreateVenueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondValidationError(c, err.Error())
		return
	}

	v, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, api.Response{
		Success: true,
		Data:    v,
	})
}

// GetByID godoc
// @Summary      Get venue
// @Description  Get venue by ID
// @Tags         venues
// @Produce      json
// @Param        id   path      string  true  "Venue ID"
// @Success      200  {object}  api.Response{data=Venue}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venues/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondValidationError(c, "invalid venue ID")
		return
	}

	v, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    v,
	})
}

// List godoc
// @Summary      List venues
// @Description  Get all venues
// @Tags         venues
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Venue}
// @Failure      500  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venues [get]
func (h *Handler) List(c *gin.Context) {
	venues, err := h.service.List(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    venues,
	})
}

// Update godoc
// @Summary      Update venue
// @Description  Update venue by ID
// @Tags         venues
// @Accept       json
// @Produce      json
// @Param        id       path      string            true  "Venue ID"
// @Param        request  body      UpdateVenueRequest  true  "Fields to update"
// @Success      200      {object}  api.Response{data=Venue}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      404      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venues/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondValidationError(c, "invalid venue ID")
		return
	}

	var req UpdateVenueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondValidationError(c, err.Error())
		return
	}

	v, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    v,
	})
}

// Delete godoc
// @Summary      Delete venue
// @Description  Delete venue by ID
// @Tags         venues
// @Produce      json
// @Param        id   path      string  true  "Venue ID"
// @Success      204
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/venues/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondValidationError(c, "invalid venue ID")
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func RespondValidationError(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, api.Response{
		Success: false,
		Error: &api.ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: message,
		},
	})
}

func RespondError(c *gin.Context, err error) {
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
