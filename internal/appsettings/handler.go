package appsettings

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

type updateSettingsRequest struct {
	MaxWalletsPerUser int `json:"max_wallets_per_user" binding:"required"`
}

type updatePerpDexesRequest struct {
	Dexes []string `json:"dexes"`
}

// GetSettings godoc
// @Summary      Get Telegram settings
// @Description  Get admin-configurable Telegram settings. Requires JWT + admin role.
// @Tags         admin-telegram
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=TelegramSettings}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/admin/telegram/settings [get]
func (h *Handler) GetSettings(c *gin.Context) {
	settings, err := h.service.GetTelegramSettings()
	if err != nil {
		api.RespondError(c, domain.GetAppError(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    settings,
	})
}

// UpdateSettings godoc
// @Summary      Update Telegram settings
// @Description  Update admin-configurable Telegram settings. Requires JWT + admin role.
// @Tags         admin-telegram
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      updateSettingsRequest  true  "Settings to update"
// @Success      200      {object}  api.Response{data=TelegramSettings}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      401      {object}  api.Response{error=api.ErrorBody}
// @Failure      403      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/admin/telegram/settings [patch]
func (h *Handler) UpdateSettings(c *gin.Context) {
	var req updateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}

	raw, ok := c.Get("user_id")
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "authentication required"))
		return
	}
	userID, err := uuid.Parse(raw.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token identity"))
		return
	}

	if err := h.service.SetMaxWalletsPerUser(c.Request.Context(), req.MaxWalletsPerUser, userID); err != nil {
		api.RespondError(c, domain.GetAppError(err))
		return
	}

	settings := TelegramSettings{MaxWalletsPerUser: req.MaxWalletsPerUser}
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    settings,
	})
}

// GetPerpDexes godoc
// @Summary      Get perp dex list
// @Description  Get admin-configurable perp dex list. Requires JWT + admin role.
// @Tags         admin-perp-dexes
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=PerpDexSettings}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/admin/perp-dexes [get]
func (h *Handler) GetPerpDexes(c *gin.Context) {
	dexes, err := h.service.GetPerpDexes(c.Request.Context())
	if err != nil {
		api.RespondError(c, domain.GetAppError(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    PerpDexSettings{Dexes: dexes},
	})
}

// UpdatePerpDexes godoc
// @Summary      Update perp dex list
// @Description  Update admin-configurable perp dex list. Requires JWT + admin role.
// @Tags         admin-perp-dexes
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      updatePerpDexesRequest  true  "Perp dexes to update"
// @Success      200      {object}  api.Response{data=PerpDexSettings}
// @Failure      400      {object}  api.Response{error=api.ErrorBody}
// @Failure      401      {object}  api.Response{error=api.ErrorBody}
// @Failure      403      {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/admin/perp-dexes [patch]
func (h *Handler) UpdatePerpDexes(c *gin.Context) {
	var req updatePerpDexesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}

	raw, ok := c.Get("user_id")
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "authentication required"))
		return
	}
	userID, err := uuid.Parse(raw.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token identity"))
		return
	}

	if err := h.service.SetPerpDexes(c.Request.Context(), req.Dexes, userID); err != nil {
		api.RespondError(c, domain.GetAppError(err))
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    PerpDexSettings{Dexes: req.Dexes},
	})
}
