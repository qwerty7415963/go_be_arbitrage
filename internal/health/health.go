package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
)

type Checker interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	checkers map[string]Checker
	mu       sync.RWMutex
}

func NewHandler() *Handler {
	return &Handler{
		checkers: make(map[string]Checker),
	}
}

func (h *Handler) Register(name string, checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[name] = checker
}

// Health godoc
// @Summary      Health check
// @Description  Check service health status including all registered checkers
// @Tags         system
// @Produce      json
// @Success      200  {object}  api.Response{data=api.HealthResponse}
// @Failure      503  {object}  api.Response{error=api.ErrorBody}
// @Router       /health [get]
func (h *Handler) Health(c *gin.Context) {
	status := "healthy"
	checks := make(map[string]string)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for name, checker := range h.checkers {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := checker.Ping(ctx); err != nil {
			checks[name] = "unhealthy"
			status = "unhealthy"
		} else {
			checks[name] = "healthy"
		}
	}

	if status == "unhealthy" {
		c.JSON(http.StatusServiceUnavailable, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "SERVICE_UNAVAILABLE",
				Message: "one or more services are unhealthy",
				Details: checks,
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data: api.HealthResponse{
			Status: status,
			Checks: checks,
		},
	})
}

// Ready godoc
// @Summary      Readiness check
// @Description  Check if service is ready to accept requests
// @Tags         system
// @Produce      json
// @Success      200  {object}  api.Response{data=api.ReadyResponse}
// @Failure      503  {object}  api.Response{error=api.ErrorBody}
// @Router       /ready [get]
func (h *Handler) Ready(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.checkers) == 0 {
		c.JSON(http.StatusOK, api.Response{
			Success: true,
			Data: api.ReadyResponse{
				Status: "ready",
			},
		})
		return
	}

	for name, checker := range h.checkers {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := checker.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, api.Response{
				Success: false,
				Error: &api.ErrorBody{
					Code:    "SERVICE_UNAVAILABLE",
					Message: "service not ready",
					Details: map[string]string{
						"service": name,
						"error":   err.Error(),
					},
				},
			})
			return
		}
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data: api.ReadyResponse{
			Status: "ready",
		},
	})
}
