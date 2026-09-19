package httpserver

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

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
) {
	s.engine.Use(middleware.RequestID())
	s.engine.Use(middleware.Logger(s.logger))
	s.engine.Use(middleware.CORS(s.config.CORS.AllowOrigins))

	s.engine.GET("/health", healthHandler.Health)
	s.engine.GET("/ready", healthHandler.Ready)

	v1 := s.engine.Group("/api/v1")
	{
		v1.GET("/ping", pingHandler)

		// Auth
		authRoutes := v1.Group("/auth")
		{
			authRoutes.POST("/register", authHandler.Register)
			authRoutes.POST("/login", middleware.LoginRateLimit(5, 1*time.Minute), authHandler.Login)
			authRoutes.POST("/refresh", authHandler.Refresh)

			// Web3 wallet auth
			authRoutes.POST("/wallet/nonce", middleware.LoginRateLimit(10, 1*time.Minute), web3Handler.GetNonce)
			authRoutes.POST("/wallet/verify", middleware.LoginRateLimit(10, 1*time.Minute), web3Handler.Verify)

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

		// Venues (admin only)
		venues := v1.Group("/venues")
		venues.Use(middleware.JWT(authService))
		venues.Use(middleware.RequireRole("admin"))
		{
			venues.GET("", venueHandler.List)
			venues.POST("", venueHandler.Create)
			venues.GET("/:id", venueHandler.GetByID)
			venues.PUT("/:id", venueHandler.Update)
			venues.DELETE("/:id", venueHandler.Delete)
		}

		// Instruments
		instruments := v1.Group("/instruments")
		{
			instruments.GET("", instrumentHandler.List)
			instruments.POST("", instrumentHandler.Create)
			instruments.GET("/tradable", instrumentHandler.ListTradable)
			instruments.GET("/:id", instrumentHandler.GetByID)
			instruments.PUT("/:id", instrumentHandler.Update)
			instruments.DELETE("/:id", instrumentHandler.Delete)
			instruments.PUT("/:id/trading", instrumentHandler.EnableTrading)
		}

		// Venue Instruments
		venueInstruments := v1.Group("/venue-instruments")
		{
			venueInstruments.GET("", instrumentHandler.ListVenueInstruments)
			venueInstruments.POST("", instrumentHandler.CreateVenueInstrument)
		}

		// Market Data
		marketData := v1.Group("/market")
		{
			marketData.GET("/subscribe", marketHandler.Subscribe)
			marketData.GET("/trades", marketHandler.GetTrades)
			marketData.GET("/ticker", marketHandler.GetTicker)
			marketData.GET("/funding", marketHandler.GetFunding)
			marketData.GET("/subscriptions", marketHandler.GetSubscriptions)
		}

		// Order Book
		orderbookRoutes := v1.Group("/orderbook")
		{
			orderbookRoutes.GET("/depth", orderbookHandler.GetOrderBook)
			orderbookRoutes.GET("/health", orderbookHandler.GetHealth)
			orderbookRoutes.GET("/tradable", orderbookHandler.GetTradable)
			orderbookRoutes.POST("/resync", orderbookHandler.RequestResync)
			orderbookRoutes.GET("/ws", orderbookHandler.SubscribeWS)
		}

		// Unified State
		unifiedRoutes := v1.Group("/unified")
		{
			unifiedRoutes.GET("/instruments", unifiedHandler.GetInstruments)
			unifiedRoutes.GET("/instruments/:id", unifiedHandler.GetInstrument)
			unifiedRoutes.GET("/instruments/:id/depth", unifiedHandler.GetExecutableDepth)
			unifiedRoutes.GET("/health", unifiedHandler.GetHealth)
			unifiedRoutes.GET("/ws", unifiedHandler.SubscribeWS)
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

		// Strategy Engine
		strategyHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Risk Engine
		riskHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Execution Engine
		executionHandler.RegisterRoutes(v1, middleware.JWT(authService))

		// Reconciliation Engine
		reconciliationHandler.RegisterRoutes(v1, middleware.JWT(authService))

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
