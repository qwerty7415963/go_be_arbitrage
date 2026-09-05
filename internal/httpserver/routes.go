package httpserver

import (
	"github.com/gin-gonic/gin"
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
		v1.GET("/ping", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "pong"})
		})
	}
}
