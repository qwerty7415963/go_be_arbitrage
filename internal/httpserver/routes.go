package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver/middleware"
)

func (s *Server) SetupRoutes(healthHandler *health.Handler) {
	s.engine.Use(middleware.RequestID())
	s.engine.Use(middleware.Logger(s.logger))

	s.engine.GET("/health", healthHandler.Health)
	s.engine.GET("/ready", healthHandler.Ready)

	v1 := s.engine.Group("/api/v1")
	{
		v1.GET("/ping", pingHandler)
	}

	s.engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

// Ping godoc
// @Summary      Ping test
// @Description  Simple ping endpoint to test connectivity
// @Tags         system
// @Produce      json
// @Success      200  {object}  api.Response{data=api.PingResponse}
// @Router       /api/v1/ping [get]
func pingHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pong"})
}
