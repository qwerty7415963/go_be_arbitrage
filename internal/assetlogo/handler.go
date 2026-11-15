package assetlogo

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Handler struct {
	resolver *Resolver
	storage  LogoStorage
	repo     *Repository
	cache    *Cache
}

func NewHandler(resolver *Resolver, storage LogoStorage, repo *Repository, cache *Cache) *Handler {
	return &Handler{
		resolver: resolver,
		storage:  storage,
		repo:     repo,
		cache:    cache,
	}
}

type refreshLogosRequest struct {
	AssetIDs []string `json:"asset_ids"`
	All      bool     `json:"all"`
}

// ServeLogo godoc
// @Summary      Get asset logo
// @Description  Serve a stored asset logo file.
// @Tags         assets
// @Produce      image/png,image/webp,image/jpeg
// @Param        assetId  path      string  true  "Asset ID"
// @Success      200
// @Failure      404  {object}  api.Response
// @Router       /api/v1/assets/logos/{assetId} [get]
func (h *Handler) ServeLogo(c *gin.Context) {
	assetID, err := uuid.Parse(c.Param("assetId"))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid asset id"))
		return
	}
	// find any logo row
	foundLogo := h.firstLogoRow(c, assetID)
	if foundLogo == nil || foundLogo.StoragePath == nil {
		c.Status(http.StatusNotFound)
		return
	}
	path := *foundLogo.StoragePath
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if foundLogo.ETag != nil && c.GetHeader("If-None-Match") == *foundLogo.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	if foundLogo.ContentType != nil {
		c.Header("Content-Type", *foundLogo.ContentType)
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	if foundLogo.ETag != nil {
		c.Header("ETag", *foundLogo.ETag)
	}
	c.File(path)
}

func (h *Handler) firstLogoRow(c *gin.Context, assetID uuid.UUID) *AssetLogo {
	if h.resolver != nil {
		if l := h.resolver.firstLogo(c.Request.Context(), assetID); l != nil {
			return l
		}
	}
	if h.repo == nil {
		return nil
	}
	for _, name := range []string{"trustwallet", "logodev"} {
		if l, err := h.repo.GetLogo(c.Request.Context(), assetID, name); err == nil {
			return l
		}
	}
	return nil
}

// RefreshLogos godoc
// @Summary      Refresh asset logos
// @Description  Enqueue logo resolution jobs. Requires JWT + admin.
// @Tags         admin-assets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      refreshLogosRequest  false  "Refresh request"
// @Success      200  {object}  api.Response
// @Failure      400  {object}  api.Response
// @Failure      401  {object}  api.Response
// @Failure      403  {object}  api.Response
// @Router       /api/v1/admin/assets/logos/refresh [post]
func (h *Handler) RefreshLogos(c *gin.Context) {
	var req refreshLogosRequest
	_ = c.ShouldBindJSON(&req)

	enqueued := 0
	skipped := 0

	if req.All {
		if h.repo == nil {
			c.JSON(http.StatusOK, api.Response{Success: true, Data: map[string]int{"enqueued": 0, "skipped": 0}})
			return
		}
		assets, err := h.repo.ListAssets(c.Request.Context())
		if err != nil {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list assets", err))
			return
		}
		for _, a := range assets {
			if h.resolver != nil && h.resolver.Enqueue(a.ID) {
				enqueued++
			} else {
				skipped++
			}
		}
	} else {
		for _, s := range req.AssetIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				skipped++
				continue
			}
			if h.resolver != nil && h.resolver.Enqueue(id) {
				enqueued++
			} else {
				skipped++
			}
		}
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    map[string]int{"enqueued": enqueued, "skipped": skipped},
	})
}

// ListLogos godoc
// @Summary      List asset logos by status
// @Description  List asset logos filtered by status. Requires JWT + admin.
// @Tags         admin-assets
// @Produce      json
// @Security     BearerAuth
// @Param        status  query     string  false  "Status filter"  Enums(missing,error,pending)
// @Param        limit   query     int     false  "Page size"  default(20)
// @Param        offset  query     int     false  "Offset"     default(0)
// @Success      200  {object}  api.Response
// @Failure      400  {object}  api.Response
// @Failure      401  {object}  api.Response
// @Failure      403  {object}  api.Response
// @Router       /api/v1/admin/assets/logos [get]
func (h *Handler) ListLogos(c *gin.Context) {
	status := c.Query("status")
	if status == "" {
		status = string(LogoStatusMissing)
	}
	switch status {
	case string(LogoStatusMissing), string(LogoStatusError), string(LogoStatusPending), string(LogoStatusFound):
	default:
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid status"))
		return
	}
	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if h.repo == nil {
		api.RespondList(c, []*AssetLogo{}, &api.Meta{Limit: limit, Offset: offset, HasMore: false})
		return
	}
	logos, err := h.repo.ListLogosByStatus(c.Request.Context(), status, limit, offset)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list logos", err))
		return
	}
	api.RespondList(c, logos, &api.Meta{Limit: limit, Offset: offset, HasMore: len(logos) == limit})
}
