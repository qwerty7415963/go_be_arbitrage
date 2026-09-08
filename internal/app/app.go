package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/database"
	"github.com/qwerty7415963/go_be_arbitrage/internal/exchangeconfig"
	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/market"
	"github.com/qwerty7415963/go_be_arbitrage/internal/orderbook"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
	"github.com/qwerty7415963/go_be_arbitrage/internal/storage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/ws"
)

type App struct {
	config            *config.Config
	logger            *logger.Logger
	database          *database.Database
	httpServer        *httpserver.Server
	hub               *ws.Hub
	auth              *auth.Service
	health            *health.Handler
	venueService      *venue.Service
	instrumentService *instrument.Service
	marketService     *market.Service
	orderbookService  *orderbook.Service
	unifiedService    *unifiedstate.Service
	storageHandler    *storage.Handler
	exchangeConfigHandler *exchangeconfig.Handler
	authHandler       *auth.Handler
}

func New(cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	hub := ws.NewHub()
	go hub.Run()

	authRepo := auth.NewRepository(db.Pool())
	authService := auth.NewService(&cfg.Auth, authRepo)

	if err := authService.EnsureAdmin(ctx, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword); err != nil {
		log.Error("failed to ensure admin user", "error", err)
	}

	healthHandler := health.NewHandler()
	healthHandler.Register("database", db)

	venueRepo := venue.NewRepository(db.Pool())
	venueService := venue.NewService(venueRepo)
	venueHandler := venue.NewHandler(venueService)

	instrumentRepo := instrument.NewRepository(db.Pool())
	instrumentService := instrument.NewService(instrumentRepo)
	instrumentHandler := instrument.NewHandler(instrumentService)

	rawEventRepo := market.NewRawEventRepository(db.Pool())
	normalizedEventRepo := market.NewNormalizedEventRepository(db.Pool())
	connManager := market.NewConnectionManager()
	subManager := market.NewSubscriptionManager()
	marketService := market.NewService(rawEventRepo, normalizedEventRepo, connManager, subManager)
	marketHandler := market.NewHandler(marketService)

	orderbookRepo := orderbook.NewRepository(db.Pool())
	orderbookService := orderbook.NewService(orderbookRepo)
	orderbookHandler := orderbook.NewHandler(orderbookService)

	unifiedRepo := unifiedstate.NewRepository(db.Pool())
	unifiedService := unifiedstate.NewService(unifiedRepo)
	unifiedHandler := unifiedstate.NewHandler(unifiedService)

	opportunityRepo := storage.NewOpportunityRepository(db.Pool())
	decisionRepo := storage.NewDecisionRepository(db.Pool())
	auditRepo := storage.NewAuditRepository(db.Pool())
	replayReader := storage.NewReplayReader(db.Pool())
	retentionSvc := storage.NewRetentionService(db.Pool())
	storageHandler := storage.NewHandler(opportunityRepo, decisionRepo, auditRepo, replayReader, retentionSvc)

	exchangeConfigRepo := exchangeconfig.NewRepository(db.Pool())
	exchangeConfigService := exchangeconfig.NewService(exchangeConfigRepo)
	exchangeConfigHandler := exchangeconfig.NewHandler(exchangeConfigService)

	authHandler := auth.NewHandler(authService)

	httpServer := httpserver.New(cfg, log)
	httpServer.SetupRoutes(healthHandler, venueHandler, instrumentHandler, marketHandler, orderbookHandler, unifiedHandler, storageHandler, exchangeConfigHandler, authService, authHandler)

	return &App{
		config:            cfg,
		logger:            log,
		database:          db,
		httpServer:        httpServer,
		hub:               hub,
		auth:              authService,
		health:            healthHandler,
		venueService:      venueService,
		instrumentService: instrumentService,
		marketService:     marketService,
		orderbookService:  orderbookService,
		unifiedService:    unifiedService,
		storageHandler:    storageHandler,
		exchangeConfigHandler: exchangeConfigHandler,
		authHandler:       authHandler,
	}, nil
}

func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.httpServer.Start()
	}()

	a.logger.Info("application started",
		"addr", a.config.ServerAddr(),
		"mode", a.config.Server.Mode,
	)

	select {
	case <-quit:
		a.logger.Info("shutdown signal received")
	case err := <-errCh:
		a.logger.Error("server error", "error", err)
	}

	return a.Shutdown(ctx)
}

func (a *App) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down application")

	shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
		a.logger.Error("shutdown http server", "error", err)
	}

	a.database.Close()

	a.logger.Info("application shutdown complete")
	return nil
}
