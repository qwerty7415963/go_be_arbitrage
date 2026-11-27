package traderfollow

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Store interface {
	Follow(ctx context.Context, userID uuid.UUID, items []MemberInput) (int64, int64, error)
	Unfollow(ctx context.Context, userID uuid.UUID, items []MemberInput) (int64, error)
	UpdateFollow(ctx context.Context, userID uuid.UUID, venue, addr string, req UpdateFollowRequest) error
	Status(ctx context.Context, userID uuid.UUID, venue, addr string) (*FollowStatus, error)
	Keys(ctx context.Context, userID uuid.UUID) ([]FollowKey, error)
	List(ctx context.Context, userID uuid.UUID, params ListFollowsParams) ([]*FollowListItem, int, error)
	Overview(ctx context.Context, userID uuid.UUID, period string) (*FollowOverview, error)
}

type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler { return &Handler{store: store} }

func (h *Handler) RegisterRoutes(router *gin.RouterGroup, jwtMiddleware ...gin.HandlerFunc) {
	g := router.Group("/trader-follows")
	if len(jwtMiddleware) > 0 {
		g.Use(jwtMiddleware[0])
	}
	g.GET("", h.List)
	g.GET("/keys", h.Keys)
	g.GET("/overview", h.Overview)
	g.POST("", h.Follow)
	g.DELETE("", h.Unfollow)
	g.GET("/:venue/:address", h.Status)
	g.PUT("/:venue/:address", h.FollowOne)
	g.DELETE("/:venue/:address", h.UnfollowOne)
	g.PATCH("/:venue/:address/preferences", h.UpdatePreferences)
}

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

func (h *Handler) List(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	page := 1
	pageSize := 20
	if raw := c.Query("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	if raw := c.Query("page_size"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	params := ListFollowsParams{
		Period:   c.DefaultQuery("period", "30D"),
		Search:   c.Query("search"),
		Sort:     c.DefaultQuery("sort", "followed_at"),
		Dir:      c.DefaultQuery("dir", "desc"),
		Page:     page,
		PageSize: pageSize,
		Filter:   c.DefaultQuery("filter", "all"),
	}
	items, total, err := h.store.List(c.Request.Context(), userID, params)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if items == nil {
		items = []*FollowListItem{}
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	hasMore := page*pageSize < total
	api.RespondList(c, items, &api.Meta{
		Page:       page,
		TotalPages: totalPages,
		Total:      int64(total),
		Limit:      pageSize,
		HasMore:    hasMore,
	})
}

func (h *Handler) Keys(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	keys, err := h.store.Keys(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	if keys == nil {
		keys = []FollowKey{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": keys})
}

func (h *Handler) Overview(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "30D")
	ov, err := h.store.Overview(c.Request.Context(), userID, period)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ov})
}

type followReq struct {
	Wallets       []MemberInput `json:"wallets" binding:"required"`
	NotifyEnabled *bool         `json:"notify_enabled"`
}

func (h *Handler) Follow(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	var req followReq
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	if req.NotifyEnabled != nil {
		for i := range req.Wallets {
			if req.Wallets[i].NotifyEnabled == nil {
				v := *req.NotifyEnabled
				req.Wallets[i].NotifyEnabled = &v
			}
		}
	}
	added, skipped, err := h.store.Follow(c.Request.Context(), userID, req.Wallets)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"followed": added, "skipped": skipped}})
}

type unfollowReq struct {
	Wallets []MemberInput `json:"wallets" binding:"required"`
}

func (h *Handler) Unfollow(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	var req unfollowReq
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	n, err := h.store.Unfollow(c.Request.Context(), userID, req.Wallets)
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"unfollowed": n}})
}

func (h *Handler) Status(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	st, err := h.store.Status(c.Request.Context(), userID, c.Param("venue"), c.Param("address"))
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

func (h *Handler) FollowOne(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	added, skipped, err := h.store.Follow(c.Request.Context(), userID, []MemberInput{{
		Venue:         c.Param("venue"),
		WalletAddress: c.Param("address"),
	}})
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"followed": added, "skipped": skipped}})
}

func (h *Handler) UnfollowOne(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	n, err := h.store.Unfollow(c.Request.Context(), userID, []MemberInput{{
		Venue:         c.Param("venue"),
		WalletAddress: c.Param("address"),
	}})
	if err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"unfollowed": n}})
}

func (h *Handler) UpdatePreferences(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		return
	}
	venue := c.Param("venue")
	addr := c.Param("address")
	var req UpdateFollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid request body"))
		return
	}
	if err := h.store.UpdateFollow(c.Request.Context(), userID, venue, addr, req); err != nil {
		api.RespondError(c, appErr(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": true}})
}

func appErr(err error) *domain.AppError {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return domain.NewError(domain.ErrCodeInternal, err.Error())
}
