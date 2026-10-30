package telegram

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Handler struct {
	linker LinkService
}

func NewHandler(args ...interface{}) *Handler {
	h := &Handler{}
	for _, a := range args {
		switch v := a.(type) {
		case LinkService:
			h.linker = v
		case *Service:
			h.linker = v
		case *Repository:
			h.linker = NewService(v)
		}
	}
	if h.linker == nil {
		h.linker = NewService()
	}
	return h
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	g := router.Group("/telegram")
	if len(jwtMiddleware) > 0 {
		g.Use(jwtMiddleware[0])
	}
	g.POST("/link", h.Link)
	g.GET("/status", h.Status)
	g.DELETE("/link", h.Unlink)
}

func (h *Handler) CreateLink(c *gin.Context) { h.Link(c) }
func (h *Handler) GetStatus(c *gin.Context)  { h.Status(c) }
func (h *Handler) DeleteLink(c *gin.Context) { h.Unlink(c) }
func (h *Handler) Delete(c *gin.Context)     { h.Unlink(c) }

func (h *Handler) getUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, ok := c.Get("user_id")
	if !ok {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token identity"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) appErr(err error) *domain.AppError {
	if ae, ok := err.(*domain.AppError); ok {
		return ae
	}
	return domain.NewError(domain.ErrCodeInternal, "internal error").WithErr(err)
}

// Link godoc
// @Summary      Link Telegram account
// @Description  Generate a one-time code + deep link to bind a Telegram chat to the authenticated user. Requires JWT.
// @Tags         telegram
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      object  false  "Optional locale (vi or en), defaults to vi"
// @Success      200  {object}  api.Response{data=LinkResponse}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}  "invalid locale"
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Failure      409  {object}  api.Response{error=api.ErrorBody}  "already connected"
// @Router       /api/v1/telegram/link [post]
func (h *Handler) Link(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	var req struct {
		Locale string `json:"locale"`
	}
	_ = c.ShouldBindJSON(&req)
	locale := Locale(strings.TrimSpace(req.Locale))
	if locale == "" {
		locale = LocaleVI
	}
	if !locale.IsValid() {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid locale"))
		return
	}
	resp, err := h.linker.CreateLink(c.Request.Context(), userID, locale)
	if err != nil {
		api.RespondError(c, h.appErr(err))
		return
	}
	c.JSON(http.StatusOK, api.Response{Success: true, Data: resp})
}

// Status godoc
// @Summary      Telegram link status
// @Description  Get the current Telegram binding status for the authenticated user. Requires JWT.
// @Tags         telegram
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  api.Response{data=StatusResponse}
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/telegram/status [get]
func (h *Handler) Status(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	resp, err := h.linker.Status(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, h.appErr(err))
		return
	}
	api.RespondSuccess(c, resp)
}

// Unlink godoc
// @Summary      Unlink Telegram account
// @Description  Remove the Telegram binding for the authenticated user. Requires JWT.
// @Tags         telegram
// @Produce      json
// @Security     BearerAuth
// @Success      204
// @Failure      401  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/telegram/link [delete]
func (h *Handler) Unlink(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	if err := h.linker.Unlink(c.Request.Context(), userID); err != nil {
		api.RespondError(c, h.appErr(err))
		return
	}
	c.Status(http.StatusNoContent)
}
