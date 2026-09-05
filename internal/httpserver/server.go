package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
)

type Server struct {
	server *http.Server
	engine *gin.Engine
	config *config.Config
	logger *logger.Logger
}

func New(cfg *config.Config, log *logger.Logger) *Server {
	gin.SetMode(cfg.Server.Mode)

	engine := gin.New()
	engine.Use(gin.Recovery())

	s := &Server{
		engine: engine,
		config: cfg,
		logger: log,
	}

	s.server = &http.Server{
		Addr:         cfg.ServerAddr(),
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Engine() *gin.Engine {
	return s.engine
}

func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.config.ServerAddr())
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("start server: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.server.Shutdown(ctx)
}
