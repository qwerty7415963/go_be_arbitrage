package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/qwerty7415963/go_be_arbitrage/internal/health"
	"github.com/qwerty7415963/go_be_arbitrage/internal/httpserver/middleware"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/market"
	"github.com/qwerty7415963/go_be_arbitrage/internal/orderbook"
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
) {
	s.engine.Use(middleware.RequestID())
	s.engine.Use(middleware.Logger(s.logger))

	s.engine.GET("/health", healthHandler.Health)
	s.engine.GET("/ready", healthHandler.Ready)

	v1 := s.engine.Group("/api/v1")
	{
		v1.GET("/ping", pingHandler)

		// Venues
		venues := v1.Group("/venues")
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
