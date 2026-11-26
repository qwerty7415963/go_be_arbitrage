package tradergroup

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// validPeriod reports whether p is one of the contract period windows.
func validPeriod(p string) bool {
	switch p {
	case "1D", "7D", "30D", "90D", "ALL":
		return true
	default:
		return false
	}
}

// Stats godoc
// @Summary      Group stats
// @Description  Aggregated analytics for one owned group over a period (contract §6.4). Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string  true   "Group ID"
// @Param        period   query     string  false  "Metric window" enums(1D,7D,30D,90D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=GroupStats}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/stats [get]
func (h *Handler) Stats(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "30D")
	if !validPeriod(period) {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidPeriod, "invalid period"))
		return
	}
	stats, err := h.store.Stats(c.Request.Context(), id, userID, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, stats)
}

// Positions godoc
// @Summary      Group positions
// @Description  Current open positions aggregated by coin for one owned group (contract §6.6). Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Group ID"
// @Success      200  {object}  api.Response{data=PositionsResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/positions [get]
func (h *Handler) Positions(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	resp, err := h.store.Positions(c.Request.Context(), id, userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, resp)
}

// Performance godoc
// @Summary      Group performance series
// @Description  Account value, cumulative realized PnL and ROI-index series for one owned group (contract §6.5). Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string  true   "Group ID"
// @Param        period   query     string  false  "Metric window" enums(1D,7D,30D,90D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=PerformanceResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/performance [get]
func (h *Handler) Performance(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "30D")
	if !validPeriod(period) {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidPeriod, "invalid period"))
		return
	}
	resp, err := h.store.Performance(c.Request.Context(), id, userID, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, resp)
}

// Compare godoc
// @Summary      Compare trader groups
// @Description  Compares 2..10 owned groups over the same period (contract §6.8). Requires JWT.
// @Tags         trader-groups
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      CompareRequest  true  "Groups to compare"
// @Success      200  {object}  api.Response{data=CompareResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/compare [post]
func (h *Handler) Compare(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	var req CompareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	ids := make([]uuid.UUID, 0, len(req.GroupIDs))
	for _, raw := range req.GroupIDs {
		gid, err := uuid.Parse(raw)
		if err != nil {
			api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid group ID"))
			return
		}
		ids = append(ids, gid)
	}
	if len(ids) < 2 {
		api.RespondError(c, domain.NewError(domain.ErrCodeCompareTooFew, "compare requires at least 2 groups"))
		return
	}
	if len(ids) > 10 {
		api.RespondError(c, domain.NewError(domain.ErrCodeCompareTooMany, "compare allows at most 10 groups"))
		return
	}
	period := req.Period
	if period == "" {
		period = "30D"
	}
	if !validPeriod(period) {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidPeriod, "invalid period"))
		return
	}
	resp, err := h.store.Compare(c.Request.Context(), userID, ids, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, resp)
}

// Exposure godoc
// @Summary      Group exposure
// @Description  Per-coin exposure for one owned group (contract A6.6.1). Requires JWT.
// @Tags         trader-groups
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string  true   "Group ID"
// @Param        period   query     string  false  "Metric window" enums(1D,7D,30D,90D,ALL) default(30D)
// @Success      200  {object}  api.Response{data=ExposureResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      403  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /v1/trader-groups/{id}/exposure [get]
func (h *Handler) Exposure(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	id, ok := groupID(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "30D")
	if !validPeriod(period) {
		api.RespondError(c, domain.NewError(domain.ErrCodeInvalidPeriod, "invalid period"))
		return
	}
	resp, err := h.store.Exposure(c.Request.Context(), id, userID, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	api.RespondSuccess(c, resp)
}
