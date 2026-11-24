package tradergroup

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func (h *Handler) ListAlerts(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid group ID"))
		return
	}
	alerts, err := h.store.ListAlerts(c.Request.Context(), id, userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if alerts == nil {
		alerts = []*GroupAlert{}
	}
	api.RespondSuccess(c, alerts)
}

func (h *Handler) CreateAlert(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid group ID"))
		return
	}
	var req CreateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "name must be 1..100 characters"))
		return
	}
	if !validAlertTypes[req.Type] {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid alert type"))
		return
	}
	a, err := h.store.CreateAlert(c.Request.Context(), id, userID, req)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondCreated(c, a)
}

func (h *Handler) UpdateAlert(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	alertID, err := uuid.Parse(c.Param("alertId"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid alert ID"))
		return
	}
	var req UpdateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "name must be 1..100 characters"))
			return
		}
	}
	if req.CooldownSeconds != nil {
		if *req.CooldownSeconds < 0 || *req.CooldownSeconds > 86400 {
			api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "cooldown_seconds must be 0..86400"))
			return
		}
	}
	a, err := h.store.UpdateAlert(c.Request.Context(), alertID, userID, req)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, a)
}

func (h *Handler) DeleteAlert(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	alertID, err := uuid.Parse(c.Param("alertId"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid alert ID"))
		return
	}
	if err := h.store.DeleteAlert(c.Request.Context(), alertID, userID); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.Status(http.StatusNoContent)
}
