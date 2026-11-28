package httpserver

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/qwerty7415963/go_be_arbitrage/internal/appsettings"
	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/execution"
	"github.com/qwerty7415963/go_be_arbitrage/internal/fundingarbitrage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver/middleware"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/market"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
	"github.com/qwerty7415963/go_be_arbitrage/internal/orderbook"
	"github.com/qwerty7415963/go_be_arbitrage/internal/reconciliation"
	"github.com/qwerty7415963/go_be_arbitrage/internal/risk"
	"github.com/qwerty7415963/go_be_arbitrage/internal/storage"
	"github.com/qwerty7415963/go_be_arbitrage/internal/strategy"
	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
	"github.com/qwerty7415963/go_be_arbitrage/internal/traderfollow"
	"github.com/qwerty7415963/go_be_arbitrage/internal/tradergroup"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
)

func (s *Server) SetupRoutes(
	healthHandler *health.Handler,
	venueHandler *venue.Handler,
	instrumentHandler *instrument.Handler,
	marketHandler *market.Handler,
	orderbookHandler *orderbook.Handler,
	unifiedHandler *unifiedstate.Handler,
	storageHandler *storage.Handler,
	authService *auth.Service,
	authHandler *auth.Handler,
	web3Handler *auth.Web3Handler,
	fundingArbitrageHandler *fundingarbitrage.Handler,
	opportunityHandler *opportunity.Handler,
	strategyHandler *strategy.Handler,
	riskHandler *risk.Handler,
	executionHandler *execution.Handler,
	reconciliationHandler *reconciliation.Handler,
	traderHandler *trader.Handler,
	traderGroupHandler *tradergroup.Handler,
	traderFollowHandler *traderfollow.Handler,
	telegramHandler *telegram.Handler,
	appSettingsHandler *appsettings.Handler,
	assetLogoHandler *assetlogo.Handler,
) {
	s.SetupPublicRoutes(healthHandler, instrumentHandler, marketHandler, orderbookHandler, unifiedHandler, authService, authHandler, web3Handler, fundingArbitrageHandler, opportunityHandler, traderHandler, traderGroupHandler, traderFollowHandler, telegramHandler, assetLogoHandler)
	s.SetupAdminRoutes(venueHandler, instrumentHandler, marketHandler, orderbookHandler, storageHandler, authService, strategyHandler, riskHandler, executionHandler, reconciliationHandler, traderHandler, appSettingsHandler, assetLogoHandler)
}

func (s *Server) trustedProxies() []string {
	if s.config.Server.TrustedProxies != nil {
		return s.config.Server.TrustedProxies
	}
	return s.config.TrustedProxies
}

func (s *Server) SetupPublicRoutes(
	healthHandler *health.Handler,
	instrumentHandler *instrument.Handler,
	marketHandler *market.Handler,
	orderbookHandler *orderbook.Handler,
	unifiedHandler *unifiedstate.Handler,
	authService *auth.Service,
	authHandler *auth.Handler,
	web3Handler *auth.Web3Handler,
	fundingArbitrageHandler *fundingarbitrage.Handler,
	opportunityHandler *opportunity.Handler,
	traderHandler *trader.Handler,
	traderGroupHandler *tradergroup.Handler,
	traderFollowHandler *traderfollow.Handler,
	telegramHandler *telegram.Handler,
	assetLogoHandler *assetlogo.Handler,
) {
	s.publicEngine.Use(middleware.RequestID())
	s.publicEngine.Use(middleware.Logger(s.logger))
	s.publicEngine.Use(middleware.CORS(s.config.CORS.AllowOrigins))
	s.publicEngine.SetTrustedProxies(s.trustedProxies())

	s.publicEngine.GET("/health", healthHandler.Health)
	s.publicEngine.GET("/ready", healthHandler.Ready)

	v1 := s.publicEngine.Group("/v1")
	v1.Use(middleware.LocaleMiddleware())
	{
		v1.GET("/ping", pingHandler)

		// Auth
		authRoutes := v1.Group("/auth")
		{
			authRoutes.POST("/register", middleware.RegisterRateLimit(), authHandler.Register)
			authRoutes.POST("/login", middleware.LoginRateLimitFull(), authHandler.Login)
			authRoutes.POST("/refresh", middleware.RefreshRateLimit(), authHandler.Refresh)

			// Web3 wallet auth
			authRoutes.POST("/wallet/nonce", middleware.WalletRateLimit(), web3Handler.GetNonce)
			authRoutes.POST("/wallet/verify", middleware.WalletRateLimit(), web3Handler.Verify)

			// Protected auth routes
			authProtected := authRoutes.Group("")
			authProtected.Use(middleware.JWT(authService))
			{
				authProtected.POST("/logout", authHandler.Logout)
				authProtected.POST("/change-password", authHandler.ChangePassword)
				authProtected.GET("/me", authHandler.Me)

				// Web3 wallet management
				authProtected.POST("/wallet/link", web3Handler.LinkWallet)
				authProtected.DELETE("/wallet/:wallet_id", web3Handler.UnlinkWallet)
				authProtected.GET("/wallet/list", web3Handler.ListWallets)
			}
		}

		// Public routes (no auth)
		public := v1.Group("/public")
		{
			public.GET("/venues", fundingArbitrageHandler.ListPerpVenues)
		}

		// Funding Arbitrage
		v1.GET("/funding/arbitrage", fundingArbitrageHandler.GetFundingArbitrage)

		// Opportunity Scanner
		opportunityHandler.RegisterRoutes(v1)

		// Market Data reads
		marketData := v1.Group("/market")
		{
			marketData.GET("/trades", marketHandler.GetTrades)
			marketData.GET("/ticker", marketHandler.GetTicker)
			marketData.GET("/funding", marketHandler.GetFunding)
			marketData.GET("/subscriptions", marketHandler.GetSubscriptions)
		}

		// Order Book reads + WS
		orderbookRoutes := v1.Group("/orderbook")
		{
			orderbookRoutes.GET("/depth", orderbookHandler.GetOrderBook)
			orderbookRoutes.GET("/health", orderbookHandler.GetHealth)
			orderbookRoutes.GET("/tradable", orderbookHandler.GetTradable)
			orderbookRoutes.GET("/ws", orderbookHandler.SubscribeWS)
		}

		// Unified State reads + WS
		unifiedRoutes := v1.Group("/unified")
		{
			unifiedRoutes.GET("/instruments", unifiedHandler.GetInstruments)
			unifiedRoutes.GET("/instruments/:id", unifiedHandler.GetInstrument)
			unifiedRoutes.GET("/instruments/:id/depth", unifiedHandler.GetExecutableDepth)
			unifiedRoutes.GET("/health", unifiedHandler.GetHealth)
			unifiedRoutes.GET("/ws", unifiedHandler.SubscribeWS)
		}

		// Instruments reads
		instruments := v1.Group("/instruments")
		{
			instruments.GET("", instrumentHandler.List)
			instruments.GET("/tradable", instrumentHandler.ListTradable)
			instruments.GET("/:id", instrumentHandler.GetByID)
		}

		// Venue Instruments reads
		venueInstruments := v1.Group("/venue-instruments")
		{
			venueInstruments.GET("", instrumentHandler.ListVenueInstruments)
		}

		// Trader Scanner
		traderHandler.RegisterRoutes(v1, Timeout(55*time.Second))
		traderGroupHandler.RegisterRoutes(v1, middleware.JWT(authService))
		if traderFollowHandler != nil {
			traderFollowHandler.RegisterRoutes(v1, middleware.JWT(authService))
		}
		if telegramHandler != nil {
			telegramHandler.RegisterRoutes(v1, middleware.JWT(authService))
		}

		// Asset Logos read
		if assetLogoHandler != nil {
			assetLogoRoutes := v1.Group("/assets")
			{
				assetLogoRoutes.GET("/logos/:assetId", assetLogoHandler.ServeLogo)
			}
		}
	}
}

func (s *Server) SetupAdminRoutes(
	venueHandler *venue.Handler,
	instrumentHandler *instrument.Handler,
	marketHandler *market.Handler,
	orderbookHandler *orderbook.Handler,
	storageHandler *storage.Handler,
	authService *auth.Service,
	strategyHandler *strategy.Handler,
	riskHandler *risk.Handler,
	executionHandler *execution.Handler,
	reconciliationHandler *reconciliation.Handler,
	traderHandler *trader.Handler,
	appSettingsHandler *appsettings.Handler,
	assetLogoHandler *assetlogo.Handler,
) {
	s.adminEngine.Use(middleware.RequestID())
	s.adminEngine.Use(middleware.Logger(s.logger))
	s.adminEngine.Use(middleware.CORS(s.config.CORS.AllowOrigins))
	s.adminEngine.SetTrustedProxies(nil)
	s.adminEngine.Use(middleware.JWT(authService))
	s.adminEngine.Use(middleware.RequireRole("admin"))

	v1 := s.adminEngine.Group("/v1")
	v1.Use(middleware.LocaleMiddleware())
	{
		// Venues (all)
		venues := v1.Group("/venues")
		{
			venues.GET("", venueHandler.List)
			venues.POST("", venueHandler.Create)
			venues.GET("/:id", venueHandler.GetByID)
			venues.PUT("/:id", venueHandler.Update)
			venues.DELETE("/:id", venueHandler.Delete)
		}

		// Instruments writes
		instruments := v1.Group("/instruments")
		{
			instruments.POST("", instrumentHandler.Create)
			instruments.PUT("/:id", instrumentHandler.Update)
			instruments.DELETE("/:id", instrumentHandler.Delete)
			instruments.PUT("/:id/trading", instrumentHandler.EnableTrading)
		}

		// Venue Instruments writes
		venueInstruments := v1.Group("/venue-instruments")
		{
			venueInstruments.POST("", instrumentHandler.CreateVenueInstrument)
		}

		// Market subscribe (admin)
		marketData := v1.Group("/market")
		{
			marketData.GET("/subscribe", marketHandler.Subscribe)
		}

		// Orderbook resync
		orderbookRoutes := v1.Group("/orderbook")
		{
			orderbookRoutes.POST("/resync", orderbookHandler.RequestResync)
		}

		// Admin traders
		adminTraders := v1.Group("/admin/traders")
		traderHandler.RegisterAdminRoutes(adminTraders)

		if appSettingsHandler != nil {
			adminTelegram := v1.Group("/admin/telegram/settings")
			{
				adminTelegram.GET("", appSettingsHandler.GetSettings)
				adminTelegram.PATCH("", appSettingsHandler.UpdateSettings)
			}
			adminPerpDexes := v1.Group("/admin/perp-dexes")
			{
				adminPerpDexes.GET("", appSettingsHandler.GetPerpDexes)
				adminPerpDexes.PATCH("", appSettingsHandler.UpdatePerpDexes)
			}
			adminSettings := v1.Group("/admin/settings")
			{
				adminSettings.GET("", appSettingsHandler.GetAdminSettings)
				adminSettings.PATCH("", appSettingsHandler.UpdateAdminSettings)
			}
		}

		// Asset Logos admin
		if assetLogoHandler != nil {
			adminAssetLogos := v1.Group("/admin/assets/logos")
			{
				adminAssetLogos.POST("/refresh", assetLogoHandler.RefreshLogos)
				adminAssetLogos.GET("", assetLogoHandler.ListLogos)
			}
		}

		// Storage & Audit
		storageRoutes := v1.Group("/storage")
		{
			storageRoutes.GET("/opportunities", storageHandler.ListOpportunities)
			storageRoutes.GET("/opportunities/:id", storageHandler.GetOpportunity)
			storageRoutes.GET("/decisions/strategy/:instance_id", storageHandler.ListStrategyDecisions)
			storageRoutes.GET("/decisions/risk/:instance_id", storageHandler.ListRiskDecisions)
			storageRoutes.GET("/audit/:tenant_id", storageHandler.ListAuditEvents)
			storageRoutes.GET("/replay/market", storageHandler.ReplayMarketEvents)
			storageRoutes.POST("/retention/cleanup", storageHandler.CleanupData)
		}

		// Strategy Engine
		strategyHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Risk Engine
		riskHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Execution Engine
		executionHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Reconciliation Engine
		reconciliationHandler.RegisterRoutes(v1, middleware.JWT(authService))
	}

	s.adminEngine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

// Ping godoc
// @Summary      Ping test
// @Description  Simple ping endpoint to test connectivity
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /v1/ping [get]
func pingHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pong"})
}
