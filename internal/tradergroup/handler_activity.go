package tradergroup

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func (h *Handler) Activity(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	f := ActivityFilter{}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid limit"))
			return
		}
		f.Limit = n
	}
	f.Cursor = c.Query("cursor")
	f.Wallet = c.Query("wallet")
	f.Asset = c.Query("asset")
	f.Type = c.Query("type")
	if raw := c.Query("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid from"))
			return
		}
		f.From = &t
	}
	if raw := c.Query("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeInvalidFilter, "invalid to"))
			return
		}
		f.To = &t
	}
	page, err := h.store.Activity(c.Request.Context(), id, userID, f)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if page == nil {
		page = &ActivityPage{Items: []ActivityEvent{}, Sources: map[string]bool{}}
	}
	if page.Items == nil {
		page.Items = []ActivityEvent{}
	}
	api.RespondSuccess(c, page)
}
