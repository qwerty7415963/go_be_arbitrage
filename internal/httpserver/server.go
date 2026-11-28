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
	publicServer *http.Server
	adminServer  *http.Server
	server       *http.Server // alias for public
	publicEngine *gin.Engine
	adminEngine  *gin.Engine
	engine       *gin.Engine // alias for public
	config       *config.Config
	logger       *logger.Logger
}

func New(cfg *config.Config, log *logger.Logger) *Server {
	gin.SetMode(cfg.Server.Mode)

	publicEngine := gin.New()
	publicEngine.Use(gin.Recovery())

	adminEngine := gin.New()
	adminEngine.Use(gin.Recovery())

	s := &Server{
		publicEngine: publicEngine,
		adminEngine:  adminEngine,
		engine:       publicEngine,
		config:       cfg,
		logger:       log,
	}

	s.publicServer = &http.Server{
		Addr:         cfg.ServerAddr(),
		Handler:      publicEngine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	s.server = s.publicServer

	s.adminServer = &http.Server{
		Addr:         cfg.AdminServerAddr(),
		Handler:      adminEngine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Engine() *gin.Engine {
	return s.publicEngine
}

func (s *Server) AdminEngine() *gin.Engine {
	return s.adminEngine
}

func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.config.ServerAddr())
	s.logger.Info("starting admin HTTP server", "addr", s.config.AdminServerAddr())

	errCh := make(chan error, 2)
	go func() {
		if err := s.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("start admin server: %w", err)
		} else {
			errCh <- nil
		}
	}()
	go func() {
		if err := s.publicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("start server: %w", err)
		} else {
			errCh <- nil
		}
	}()
	return <-errCh
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	var err error
	if e := s.publicServer.Shutdown(ctx); e != nil {
		err = e
	}
	if e := s.adminServer.Shutdown(ctx); e != nil && err == nil {
		err = e
	}
	return err
}
