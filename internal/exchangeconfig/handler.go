package exchangeconfig

import (
	"strconv"

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
// @Summary      Create exchange config
// @Description  Create a new exchange connection config (admin only)
// @Tags         exchange-configs
// @Accept       json
// @Produce      json
// @Param        body  body      CreateExchangeConfigRequest  true  "Exchange config"
// @Success      201   {object}  api.Response{data=ExchangeConfig}
// @Failure      400   {object}  api.Response
// @Failure      409   {object}  api.Response
// @Router       /api/v1/exchange-configs [post]
func (h *Handler) Create(c *gin.Context) {
	var req CreateExchangeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	userIDStr, _ := c.Get("user_id")
	userID, _ := uuid.Parse(userIDStr.(string))

	config := &ExchangeConfig{
		VenueID:      req.VenueID,
		ExchangeName: req.ExchangeName,
		RestBaseURL:  req.RestBaseURL,
		WsURL:        req.WsURL,
		RateLimitRPM: req.RateLimitRPM,
		TimeoutMs:    req.TimeoutMs,
		CreatedBy:    &userID,
	}

	if err := h.service.Create(c.Request.Context(), config); err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}

	api.RespondCreated(c, config)
}

// List godoc
// @Summary      List exchange configs
// @Description  List all exchange configs (admin only)
// @Tags         exchange-configs
// @Accept       json
// @Produce      json
// @Param        limit   query    int     false  "Page size"  default(20)
// @Param        offset  query    int     false  "Offset"     default(0)
// @Success      200     {object}  api.Response{data=[]ExchangeConfig,meta=api.Meta}
// @Failure      500     {object}  api.Response
// @Router       /api/v1/exchange-configs [get]
func (h *Handler) List(c *gin.Context) {
	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	configs, err := h.service.List(c.Request.Context(), limit, offset)
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}

	api.RespondList(c, configs, &api.Meta{
		Cursor:  strconv.Itoa(offset + limit),
		HasMore: len(configs) == limit,
		Limit:   limit,
	})
}

// GetByID godoc
// @Summary      Get exchange config
// @Description  Get exchange config by ID (admin only)
// @Tags         exchange-configs
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Exchange Config ID"
// @Success      200  {object}  api.Response{data=ExchangeConfig}
// @Failure      404  {object}  api.Response
// @Router       /api/v1/exchange-configs/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid exchange config ID"))
		return
	}

	config, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}

	api.RespondSuccess(c, config)
}

// Update godoc
// @Summary      Update exchange config
// @Description  Update exchange config (admin only)
// @Tags         exchange-configs
// @Accept       json
// @Produce      json
// @Param        id    path      string                       true  "Exchange Config ID"
// @Param        body  body      UpdateExchangeConfigRequest   true  "Update fields"
// @Success      200   {object}  api.Response{data=ExchangeConfig}
// @Failure      400   {object}  api.Response
// @Failure      404   {object}  api.Response
// @Router       /api/v1/exchange-configs/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid exchange config ID"))
		return
	}

	var req UpdateExchangeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	config, err := h.service.Update(c.Request.Context(), id, &req)
	if err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}

	api.RespondSuccess(c, config)
}

// Delete godoc
// @Summary      Delete exchange config
// @Description  Delete exchange config (admin only)
// @Tags         exchange-configs
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Exchange Config ID"
// @Success      204  "No Content"
// @Failure      404  {object}  api.Response
// @Router       /api/v1/exchange-configs/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid exchange config ID"))
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		api.RespondError(c, err.(*domain.AppError))
		return
	}

	api.RespondNoContent(c)
}
