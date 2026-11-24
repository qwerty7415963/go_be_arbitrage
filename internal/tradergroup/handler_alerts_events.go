package tradergroup

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func (h *Handler) ListAlertEvents(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid group ID"))
		return
	}
	repo, ok := h.store.(*Repository)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "not supported"))
		return
	}
	unread := false
	if raw := c.Query("unread"); raw != "" {
		switch raw {
		case "true", "1":
			unread = true
		case "false", "0":
			unread = false
		default:
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid unread"))
			return
		}
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid limit"))
			return
		}
		limit = n
	}
	page, err := repo.ListAlertEvents(c.Request.Context(), id, userID, unread, limit, c.Query("cursor"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if page == nil {
		page = &AlertEventPage{Items: []AlertEvent{}}
	}
	if page.Items == nil {
		page.Items = []AlertEvent{}
	}
	api.RespondSuccess(c, page)
}

func (h *Handler) MarkAllAlertEventsRead(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid group ID"))
		return
	}
	repo, ok := h.store.(*Repository)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "not supported"))
		return
	}
	n, err := repo.MarkAllAlertEventsRead(c.Request.Context(), id, userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, map[string]int{"updated": n})
}

func (h *Handler) UnreadAlertCount(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	repo, ok := h.store.(*Repository)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "not supported"))
		return
	}
	total, counts, err := repo.UnreadAlertCount(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, map[string]any{"total": total, "groups": counts})
}

func (h *Handler) MarkAlertEventRead(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	eventID, err := uuid.Parse(c.Param("eventId"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid event ID"))
		return
	}
	repo, ok := h.store.(*Repository)
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeInternal, "not supported"))
		return
	}
	if err := repo.MarkAlertEventRead(c.Request.Context(), eventID, userID); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.Status(http.StatusNoContent)
}
