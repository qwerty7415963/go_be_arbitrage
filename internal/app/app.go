package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/appsettings"
	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo/providers"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/collector"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/database"
	"github.com/qwerty7415963/go_be_arbitrage/internal/execution"
	"github.com/qwerty7415963/go_be_arbitrage/internal/fundingarbitrage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/market"
	"github.com/qwerty7415963/go_be_arbitrage/internal/notify"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
	"github.com/qwerty7415963/go_be_arbitrage/internal/orderbook"
	"github.com/qwerty7415963/go_be_arbitrage/internal/reconciliation"
	"github.com/qwerty7415963/go_be_arbitrage/internal/risk"
	"github.com/qwerty7415963/go_be_arbitrage/internal/storage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/strategy"
	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
	"github.com/qwerty7415963/go_be_arbitrage/internal/tradergroup"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"

	"github.com/google/uuid"
)

type App struct {
	config             *config.Config
	logger             *logger.Logger
	database           *database.Database
	httpServer         *httpserver.Server
	cancel             context.CancelFunc
	auth               *auth.Service
	health             *health.Handler
	venueService       *venue.Service
	instrumentService  *instrument.Service
	marketService      *market.Service
	orderbookService   *orderbook.Service
	unifiedService     *unifiedstate.Service
	storageHandler     *storage.Handler
	retentionService   *storage.RetentionService
	assetLogoStorage   assetlogo.LogoStorage
	assetLogoCache     *assetlogo.Cache
	assetLogoResolver  *assetlogo.Resolver
	assetLogoHandler   *assetlogo.Handler
	authHandler        *auth.Handler
	collector          *collector.Collector
	opportunityService *opportunity.Service
	discoveryService   *trader.DiscoveryService
	traderSyncService  *trader.SyncService
	traderService      *trader.Service
	bucketProjector    *trader.BucketProjector
	traderVenueID      uuid.UUID
	hlClient           *hyperliquid.Client
	hlLive             *hyperliquid.LiveClient
	watcherManager     *trader.WatcherManager
	wsStream           *hyperliquid.TradeStream
	wsHarvest          *trader.WSHarvestService
	telegramService    *telegram.Service
	telegramBot        *telegram.Bot
	notifyRepo         *notify.Repository
	notifySupervisor   *notify.Supervisor
	notifyDispatcher   *notify.Dispatcher
	notifyWorker       *notify.Worker
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

	assetLogoRepo := assetlogo.NewRepository(db.Pool())
	if err := assetlogo.Seed(ctx, assetLogoRepo); err != nil {
		log.Warn("failed to seed asset logos", "error", err)
	}

	assetLogoStorage := assetlogo.NewLocalStorage(cfg.LogoDir)
	assetLogoCache := assetlogo.NewCache()
	assetLogoCache.ReadyTTL = time.Duration(cfg.LogoRefreshDays) * 24 * time.Hour
	assetLogoCache.NegativeTTL = time.Duration(cfg.LogoNegativeDays) * 24 * time.Hour

	twProvider := &providers.TrustWalletProvider{Client: assetlogo.NewSafeHTTPClient()}
	ldProvider := &providers.LogoDevProvider{SecretKey: cfg.LogoDevSecretKey, Client: assetlogo.NewSafeHTTPClient()}
	assetLogoResolver := assetlogo.NewResolver(assetLogoRepo, assetLogoStorage, assetLogoCache, twProvider, ldProvider)
	assetLogoResolver.Workers = cfg.LogoWorkers
	assetLogoHandler := assetlogo.NewHandler(assetLogoResolver, assetLogoStorage, assetLogoRepo, assetLogoCache)

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

	// Trader Scanner v1.1: generic routes + venue-scoped tables. Discovery
	// refreshes the registry from the leaderboard; the sync engine maintains
	// the period metric cache. Started in Run.
	traderRepo := trader.NewRepository(db.Pool())

	// App settings (contract v1.5): admin-configurable Telegram max wallets.
	appSettingsRepo := appsettings.NewRepository(db.Pool())
	if err := appSettingsRepo.SeedIfNotExists(ctx, appsettings.SettingKeyTelegramMaxWallets, "50"); err != nil {
		log.Warn("failed to seed app_settings", "error", err)
	}
	if err := appSettingsRepo.SeedIfNotExists(ctx, appsettings.SettingKeyPerpDexes, "xyz"); err != nil {
		log.Warn("failed to seed app_settings", "error", err)
	}
	appSettingsService := appsettings.NewService(appSettingsRepo, log)
	appSettingsHandler := appsettings.NewHandler(appSettingsService)

	traderGroupRepo := tradergroup.NewRepository(db.Pool(), appSettingsService)
	cursorSecret := []byte("trader-cursor:v1:" + cfg.Auth.JWTSecret)
	traderService := trader.NewService(traderRepo, traderGroupRepo, cursorSecret).WithAppSettings(appSettingsService).WithAssetLogoResolver(assetLogoResolver)
	bucketProjector := trader.NewBucketProjector(traderRepo, traderService)
	traderService.WithBucketProjector(bucketProjector)
	traderHandler := trader.NewHandler(traderService)
	go traderService.WarmOverviewCache(context.Background())
	traderGroupHandler := tradergroup.NewHandler(traderGroupRepo)

	// Telegram notifications (contract v1.4 phase B)
	telegramRepo := telegram.NewRepository(db.Pool())
	telegramService := telegram.NewService(telegramRepo, &cfg.Telegram)
	telegramHandler := telegram.NewHandler(telegramService)
	var telegramBot *telegram.Bot
	if cfg.Telegram.BotToken != "" && cfg.Telegram.BotUsername != "" {
		telegramBot = telegram.NewBot(&cfg.Telegram, telegramRepo, telegramService)
	} else {
		log.Warn("telegram disabled: missing token or username")
	}

	// Notify (contract v1.4 phase C)
	notifyRepo := notify.NewRepository(db.Pool())
	var notifySupervisor *notify.Supervisor
	var notifyDispatcher *notify.Dispatcher
	var notifyWorker *notify.Worker
	// LIVE-CONTRACT v1.2 Â§2 (WS-D): realtime path is one refcounted
	// WalletWatcher per watched address (first browser subscriber creates the
	// 4-feed upstream watcher; last unsubscribe tears it down) over the shared
	// browser transport GET /traders/ws?wallet=. The legacy global harvest
	// feed (addresses) stays for the scanner; browser fills also flow through
	// the harvest-derived activity path until per-wallet WS multiplexing lands.
	activityHub := trader.NewActivityHub()
	traderActivitySvc := trader.NewWSActivityService(activityHub)
	activityWS := trader.NewActivityWSHandler(activityHub)
	traderHandler.WithWS(activityWS)

	var discoverySvc *trader.DiscoveryService
	var traderSyncSvc *trader.SyncService
	var hyperliquidVenueID uuid.UUID
	var hlClient *hyperliquid.Client
	var hlLive *hyperliquid.LiveClient
	var watcherManager *trader.WatcherManager
	var wsStream *hyperliquid.TradeStream
	var wsHarvest *trader.WSHarvestService
	if venueID, verr := traderRepo.VenueIDByCode(ctx, hyperliquid.VenueCode); verr != nil {
		log.Warn("hyperliquid venue missing; trader discovery/sync disabled", "error", verr)
	} else {
		hyperliquidVenueID = venueID
		hlClient = hyperliquid.NewClient("", 30*time.Second, 2*time.Second)
		// LIVE-CONTRACT v1.2 Â§3 (WS-A): interactive reads use a SEPARATE HL
		// client (hlLive) with its OWN pacer (floor 200ms), own 429 backoff,
		// bounded concurrency (~4) and per-request ctx timeouts. Sync's
		// 2s-floor pacer is untouched. TTL 12s cache + single-flight behind
		// it; graceful degradation on HL error.
		hlLive = hyperliquid.NewLiveClient("", 12*time.Second, 200*time.Millisecond, 4)
		traderService.WithOnDemand(hlLive, trader.NewOnDemandCache(trader.LiveCacheTTL))
		appSettingsService.WithPerpDexFetcher(hlLive)
		// WS-D: one refcounted WalletWatcher per watched address (first
		// browser subscriber creates the 4-feed upstream watcher + REST
		// bootstrap RESYNCâ†’RECONCILEâ†’LIVE; last unsubscribe tears it down).
		// Shared upstream WS multiplexing is interface-ready (UpstreamDialer);
		// production runs REST-bootstrap + envelope + reconnect states on the
		// existing transport; the global harvest feed stays for the scanner.
		userStreamSink := &trader.HyperliquidUserStreamSink{}
		userStream := hyperliquid.NewUserStream("wss://api.hyperliquid.xyz/ws", userStreamSink)
		watcherManager = trader.NewWatcherManager(activityHub, hlLive, userStream, 4)
		watcherManager = watcherManager.WithPositionProvider(func(ctx context.Context, wallet string) (*trader.PositionSnapshotDTO, error) {
			var dexes []string
			if appSettingsService != nil {
				if d, err := appSettingsService.GetPerpDexes(ctx); err == nil {
					dexes = d
				}
			} else if hlLive != nil {
				if d, err := hlLive.FetchPerpDexs(ctx); err == nil {
					dexes = d
				}
			}
			if hlLive == nil {
				return nil, fmt.Errorf("upstream unavailable")
			}
			snap, err := trader.BuildLivePositionsSnapshot(ctx, hlLive, wallet, dexes)
			if err != nil {
				return nil, err
			}
			return trader.SnapshotToPositionDTO(snap, "coin", "asc", trader.DataReady), nil
		})
		userStreamSink.Mgr = watcherManager
		activityWS.WithWatcher(watcherManager)
		discoverySvc = trader.NewDiscoveryService(traderRepo,
			trader.HLDiscoveryAdapter{C: hlClient}, venueID, traderDiscoveryLimit)
		// WS-E: scanner sync keeps daily/period (+equity) only. Detail is
		// LIVE (no positions-30s sync, no durable trades writes, no POST
		// /sync priority lane). Live reads use hlLive (WS-A).
		traderSyncSvc = trader.NewSyncService(traderRepo,
			trader.HLFillAdapter{C: hlClient}, venueID, traderSyncOptions()).
			WithPortfolio(trader.HLPortfolioAdapter{C: hlClient}).
			WithPositions(trader.HLPositionAdapter{C: hlClient}).
			WithOnSyncComplete(func(ctx context.Context) { go bucketProjector.Refresh(ctx, venueID) })
		wsHarvest = trader.NewWSHarvestService(traderRepo, venueID, 500, time.Second)
		wsStream = hyperliquid.NewTradeStream("", hyperliquid.DefaultMaxCoins,
			func(evs []hyperliquid.WSTradeEvent) { wsHarvest.Submit(trader.AdaptWSBatch(evs)) })
		// A7: the same trade frames feed harvest (addresses) and realtime
		// activity (fills for watched wallets); harvest keeps working unchanged.
		wsStream.SetOnFills(func(fills []hyperliquid.WSTradeFill) { traderActivitySvc.Submit(fills) })
	}

	// Notify (contract v1.4 phase C)
	notifySupervisor = notify.NewSupervisor(watcherManager, telegramService, notifyRepo)
	notifyDispatcher = notify.NewDispatcher(notifyRepo, cfg.Telegram.AppPublicURL)
	var notifySender *telegram.Sender
	if telegramBot != nil {
		notifySender = telegram.NewSender(cfg.Telegram.BotToken)
	}
	notifyWorker = notify.NewWorker(notifyRepo, notifySender)
	if watcherManager != nil && notifyDispatcher != nil {
		watcherManager.AddObserver(func(ev trader.WalletEvent) {
			notifyDispatcher.Enqueue(ev)
		})
	}

	// Collector (lazy start - will start on first request context)
	fundingCollector := collector.NewCollector(
		db.Pool(),
		venueRepo,
		log,
		30*time.Second,
		fundingArbitrageCache,
	)

	if traderSyncSvc != nil {
		traderHandler.WithSync(traderSyncSvc)
	}

	httpServer := httpserver.New(cfg, log)
	httpServer.SetupRoutes(healthHandler, venueHandler, instrumentHandler, marketHandler, orderbookHandler, unifiedHandler, storageHandler, authService, authHandler, web3Handler, fundingArbitrageHandler, opportunityHandler, strategyHandler, riskHandler, executionHandler, reconciliationHandler, traderHandler, traderGroupHandler, telegramHandler, appSettingsHandler, assetLogoHandler)

	return &App{
		config:             cfg,
		logger:             log,
		database:           db,
		httpServer:         httpServer,
		auth:               authService,
		health:             healthHandler,
		venueService:       venueService,
		instrumentService:  instrumentService,
		marketService:      marketService,
		orderbookService:   orderbookService,
		unifiedService:     unifiedService,
		storageHandler:     storageHandler,
		retentionService:   retentionSvc,
		authHandler:        authHandler,
		collector:          fundingCollector,
		opportunityService: opportunityService,
		discoveryService:   discoverySvc,
		traderSyncService:  traderSyncSvc,
		traderService:      traderService,
		bucketProjector:    bucketProjector,
		traderVenueID:      hyperliquidVenueID,
		hlClient:           hlClient,
		hlLive:             hlLive,
		watcherManager:     watcherManager,
		wsStream:           wsStream,
		wsHarvest:          wsHarvest,
		telegramService:    telegramService,
		telegramBot:        telegramBot,
		notifyRepo:         notifyRepo,
		notifySupervisor:   notifySupervisor,
		notifyDispatcher:   notifyDispatcher,
		notifyWorker:       notifyWorker,
		assetLogoStorage:   assetLogoStorage,
		assetLogoCache:     assetLogoCache,
		assetLogoResolver:  assetLogoResolver,
		assetLogoHandler:   assetLogoHandler,
	}, nil
}

// Trader Scanner worker tuning: leaderboard refresh (39MB dump) every 15
// minutes; metric sync every 6 hours (parity with the legacy backfill, WS-E:
// scanner only â€” detail is LIVE, no positions-30s ticker); WS trade discovery
// subscribes the perp universe with hourly meta refresh.
const (
	traderDiscoveryLimit    = 500
	traderDiscoveryInterval = 15 * time.Minute
	traderSyncInterval      = 3 * time.Hour
	traderSyncWorkers       = 4
	traderWSRefreshInterval = time.Hour
	retentionInterval       = 24 * time.Hour
)

// traderSyncOptions is the production sync tuning: pooled workers over the
// shared venue limiter; see trader.DefaultSyncOptions for the rest.
func traderSyncOptions() trader.SyncOptions {
	opts := trader.DefaultSyncOptions()
	opts.Workers = traderSyncWorkers
	return opts
}

// runRetentionWorker runs full retention cleanup on start and every interval
// until ctx ends. Deletions are idempotent; per-table counts go to the log.
func runRetentionWorker(ctx context.Context, log *logger.Logger, svc *storage.RetentionService) {
	run := func() {
		res, err := svc.RunFullCleanup(ctx, storage.DefaultRetentionConfig())
		fields := []any{
			"raw_market_events", res.RawMarketEvents,
			"market_trades", res.MarketTrades,
			"market_tickers", res.MarketTickers,
			"funding_rates", res.FundingRates,
			"opportunities", res.Opportunities,
			"system_events", res.SystemEvents,
			"trader_daily", res.TraderDaily,
			"trader_equity", res.TraderEquity,
			"dead_traders", res.DeadTraders,
		}
		if err != nil {
			log.Warn("retention cleanup partial", append(fields, "error", err)...)
			return
		}
		log.Info("retention cleanup finished", fields...)
	}
	run()
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// runWSTradeDiscovery bootstraps coin subscriptions from venue metadata,
// refreshes them hourly for new listings (BE-008), and streams trades into
// the harvest service until ctx ends.
func runWSTradeDiscovery(ctx context.Context, log *logger.Logger, client *hyperliquid.Client, stream *hyperliquid.TradeStream, harvest *trader.WSHarvestService) {
	refresh := func() {
		coins, err := client.FetchPerpCoins(ctx)
		if err != nil {
			log.Warn("ws discovery: coin refresh failed", "error", err)
			return
		}
		added, err := stream.RefreshCoins(coins)
		if err != nil {
			log.Warn("ws discovery: subscription cap hit", "error", err)
			return
		}
		if len(added) > 0 {
			log.Info("ws discovery: subscribed new coins", "added", len(added))
		}
	}
	if coins, err := client.FetchPerpCoins(ctx); err != nil {
		log.Warn("ws discovery: initial coin fetch failed; retrying on schedule", "error", err)
	} else if err := stream.SetCoins(coins); err != nil {
		log.Warn("ws discovery: initial subscribe failed", "error", err)
	} else {
		log.Info("ws discovery: subscribed coins", "coins", len(coins))
	}
	go harvest.Start(ctx)
	go func() { _ = stream.Run(ctx) }()
	ticker := time.NewTicker(traderWSRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

// runLogoCleanupWorker scans the logo storage dir daily and removes orphan files.
func runLogoCleanupWorker(ctx context.Context, log *logger.Logger, r *assetlogo.Resolver) {
	run := func() {
		n, err := r.CleanupOrphans(ctx)
		if err != nil {
			log.Warn("logo cleanup partial", "error", err)
			return
		}
		log.Info("logo cleanup finished", "deleted", n)
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	defer cancel()

	// Start collector in background
	go a.collector.Start(ctx)

	// Start opportunity scanner
	go a.opportunityService.Start(ctx)

	// Start refresh token cleanup worker (every 1 hour)
	go a.auth.StartCleanupWorker(ctx, 1*time.Hour)

	// Start retention cleanup (every 24 hours): market tables with short
	// windows plus trader daily/equity past 400d. Manual trigger remains at
	// POST /v1/storage/retention/cleanup.
	if a.retentionService != nil {
		go runRetentionWorker(ctx, a.logger, a.retentionService)
	}

	if a.assetLogoResolver != nil {
		a.assetLogoResolver.Start(ctx)
		go func() {
			a.assetLogoResolver.Prewarm(ctx)
		}()
		go func() {
			runLogoCleanupWorker(ctx, a.logger, a.assetLogoResolver)
		}()
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.assetLogoResolver.Prewarm(ctx)
				}
			}
		}()
	}

	// Start Trader Scanner workers (WS-E: scanner only): leaderboard discovery
	// refreshes the registry; the sync engine maintains daily/period for the
	// scanner (6h cadence). Detail is LIVE (no positions ticker, no priority
	// lane); WalletWatcher runs on-demand per browser subscriber (no daemon).
	if a.discoveryService != nil {
		go a.discoveryService.Start(ctx, traderDiscoveryInterval)
	}
	if a.traderSyncService != nil {
		go a.traderSyncService.Start(ctx, traderSyncInterval)
	}

	// Start WS trade discovery (V1.1): harvest trade counterparties into the
	// registry continuously.
	if a.wsStream != nil {
		go runWSTradeDiscovery(ctx, a.logger, a.hlClient, a.wsStream, a.wsHarvest)
	}

	if a.traderService != nil && a.traderVenueID != uuid.Nil {
		go runOverviewSnapshotWorker(ctx, a.logger, a.traderService, a.traderVenueID)
	}
	if a.bucketProjector != nil && a.traderVenueID != uuid.Nil {
		go func() {
			a.bucketProjector.Refresh(ctx, a.traderVenueID)
			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.bucketProjector.Refresh(ctx, a.traderVenueID)
				}
			}
		}()
	}

	if a.telegramBot != nil {
		go a.telegramBot.Start(ctx)
	}
	if a.config.Telegram.BotToken != "" && a.config.Telegram.BotUsername != "" {
		if a.notifySupervisor != nil {
			go a.notifySupervisor.Start(ctx)
		}
		if a.notifyDispatcher != nil {
			go a.notifyDispatcher.Start(ctx)
		}
		if a.notifyWorker != nil {
			go a.notifyWorker.Start(ctx)
		}
	}
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

	if a.assetLogoResolver != nil {
		a.assetLogoResolver.Stop()
	}

	if a.cancel != nil {
		a.cancel()
	}

	time.Sleep(300 * time.Millisecond)

	a.database.Close()

	a.logger.Info("application shutdown complete")
	return nil
}

func runOverviewSnapshotWorker(ctx context.Context, log *logger.Logger, svc *trader.Service, venueID uuid.UUID) {
	run := func() {
		svc.RefreshOverviewSnapshots(ctx, venueID)
	}
	run()
	ticker := time.NewTicker(120 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
