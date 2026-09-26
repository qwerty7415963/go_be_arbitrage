package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/collector"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/database"
	"github.com/qwerty7415963/go_be_arbitrage/internal/execution"
	"github.com/qwerty7415963/go_be_arbitrage/internal/fundingarbitrage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/market"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
	"github.com/qwerty7415963/go_be_arbitrage/internal/orderbook"
	"github.com/qwerty7415963/go_be_arbitrage/internal/reconciliation"
	"github.com/qwerty7415963/go_be_arbitrage/internal/risk"
	"github.com/qwerty7415963/go_be_arbitrage/internal/storage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/strategy"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
	"github.com/qwerty7415963/go_be_arbitrage/internal/walletgroup"
)

type App struct {
	config            *config.Config
	logger            *logger.Logger
	database          *database.Database
	httpServer        *httpserver.Server
	auth              *auth.Service
	health            *health.Handler
	venueService      *venue.Service
	instrumentService *instrument.Service
	marketService     *market.Service
	orderbookService  *orderbook.Service
	unifiedService    *unifiedstate.Service
	storageHandler    *storage.Handler
	authHandler       *auth.Handler
	collector         *collector.Collector
	opportunityService *opportunity.Service
}

func New(cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	authRepo := auth.NewRepository(db.Pool())
	authService := auth.NewService(&cfg.Auth, authRepo)

	if err := authService.EnsureAdmin(ctx, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword); err != nil {
		log.Error("failed to ensure admin user", "error", err)
	}

	web3Repo := auth.NewWeb3Repository(db.Pool())
	web3Service := auth.NewWeb3Service(&cfg.Auth, web3Repo, authService)
	web3Handler := auth.NewWeb3Handler(web3Service)

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
	orderbookHub := orderbook.NewWsHub()
	go orderbookHub.Run()
	orderbookHandler := orderbook.NewHandler(orderbookService, orderbookHub)

	unifiedRepo := unifiedstate.NewRepository(db.Pool())
	unifiedService := unifiedstate.NewService(unifiedRepo)
	unifiedHandler := unifiedstate.NewHandler(unifiedService)

	opportunityRepo := storage.NewOpportunityRepository(db.Pool())
	decisionRepo := storage.NewDecisionRepository(db.Pool())
	auditRepo := storage.NewAuditRepository(db.Pool())
	replayReader := storage.NewReplayReader(db.Pool())
	retentionSvc := storage.NewRetentionService(db.Pool())
	storageHandler := storage.NewHandler(opportunityRepo, decisionRepo, auditRepo, replayReader, retentionSvc)

	authHandler := auth.NewHandler(authService)

	// Funding Arbitrage
	fundingArbitrageRepo := fundingarbitrage.NewRepository(db.Pool())
	fundingArbitrageCache := fundingarbitrage.NewCache(30 * time.Second)
	fundingArbitrageService := fundingarbitrage.NewService(fundingArbitrageRepo, fundingArbitrageCache)
	fundingArbitrageHandler := fundingarbitrage.NewHandler(fundingArbitrageService)

	// Opportunity Scanner
	opportunityService := opportunity.NewService(unifiedService, opportunity.DefaultScannerConfig())
	opportunityHandler := opportunity.NewHandler(opportunityService)

	// Strategy Engine
	strategyRepo := strategy.NewRepository(db.Pool())
	strategyService := strategy.NewService(strategyRepo, opportunityService)
	strategyHandler := strategy.NewHandler(strategyService)

	// Risk Engine
	riskRepo := risk.NewRepository(db.Pool())
	riskService := risk.NewService(riskRepo, risk.DefaultRiskConfig())
	riskHandler := risk.NewHandler(riskService)

	// Execution Engine
	executionRepo := execution.NewRepository(db.Pool())
	executionService := execution.NewService(executionRepo)
	executionHandler := execution.NewHandler(executionService)

	// Reconciliation Engine
	reconciliationRepo := reconciliation.NewRepository(db.Pool())
	reconciliationService := reconciliation.NewService(reconciliationRepo)
	reconciliationHandler := reconciliation.NewHandler(reconciliationService)

	// Wallet Groups (Wallet Dashboard Phase 1)
	walletGroupRepo := walletgroup.NewRepository(db.Pool())
	walletGroupService := walletgroup.NewService(walletGroupRepo)
	walletGroupHandler := walletgroup.NewHandler(walletGroupService)

	// Collector (lazy start - will start on first request context)
	fundingCollector := collector.NewCollector(
		db.Pool(),
		venueRepo,
		log,
		30*time.Second,
		fundingArbitrageCache,
	)

	httpServer := httpserver.New(cfg, log)
	httpServer.SetupRoutes(healthHandler, venueHandler, instrumentHandler, marketHandler, orderbookHandler, unifiedHandler, storageHandler, authService, authHandler, web3Handler, fundingArbitrageHandler, opportunityHandler, strategyHandler, riskHandler, executionHandler, reconciliationHandler, walletGroupHandler)

	return &App{
		config:            cfg,
		logger:            log,
		database:          db,
		httpServer:        httpServer,
		auth:              authService,
		health:            healthHandler,
		venueService:      venueService,
		instrumentService: instrumentService,
		marketService:     marketService,
		orderbookService:  orderbookService,
		unifiedService:    unifiedService,
		storageHandler:    storageHandler,
		authHandler:       authHandler,
		collector:         fundingCollector,
		opportunityService: opportunityService,
	}, nil
}

func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector in background
	go a.collector.Start(ctx)

	// Start opportunity scanner
	go a.opportunityService.Start(ctx)

	// Start refresh token cleanup worker (every 1 hour)
	go a.auth.StartCleanupWorker(ctx, 1*time.Hour)

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
