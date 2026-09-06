package market

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Handler struct {
	service *Service
	clients map[uuid.UUID]*websocket.Conn
	mu      sync.RWMutex
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
		clients: make(map[uuid.UUID]*websocket.Conn),
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Subscribe godoc
// @Summary      Subscribe to market data
// @Description  Subscribe to market data updates via WebSocket
// @Tags         market
// @Produce      json
// @Param        venue_id     query      string  true  "Venue ID"
// @Param        instrument_id query     string  true  "Instrument ID"
// @Param        channel      query      string  true  "Channel (trades, ticker, funding)"
// @Success      101  {object}  api.Response
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/market/subscribe [get]
func (h *Handler) Subscribe(c *gin.Context) {
	venueIDStr := c.Query("venue_id")
	instrumentIDStr := c.Query("instrument_id")
	channel := c.Query("channel")

	venueID, err := uuid.Parse(venueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid venue_id",
			},
		})
		return
	}

	instrumentID, err := uuid.Parse(instrumentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument_id",
			},
		})
		return
	}

	if channel == "" {
		channel = "trades"
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "WEBSOCKET_ERROR",
				Message: "failed to upgrade connection",
			},
		})
		return
	}

	clientID := uuid.New()
	h.mu.Lock()
	h.clients[clientID] = conn
	h.mu.Unlock()

	// Create connection
	venueConn := h.service.CreateConnection("unknown")
	h.service.connManager.UpdateState(venueConn.ID, ConnectionStateConnected)

	// Subscribe
	sub, err := h.service.Subscribe(c.Request.Context(), venueID, instrumentID, channel, venueConn.ID)
	if err != nil {
		conn.WriteJSON(api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    string(err.(*domain.AppError).Code),
				Message: err.(*domain.AppError).Message,
			},
		})
		conn.Close()
		return
	}

	// Send confirmation
	conn.WriteJSON(api.Response{
		Success: true,
		Data: map[string]interface{}{
			"subscription_id": sub.ID,
			"venue_id":        venueID,
			"instrument_id":   instrumentID,
			"channel":         channel,
		},
	})

	// Handle messages in goroutine
	go h.handleMessages(clientID, sub.ID)
}

func (h *Handler) handleMessages(clientID, subscriptionID uuid.UUID) {
	h.mu.RLock()
	conn, ok := h.clients[clientID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	defer func() {
		h.mu.Lock()
		delete(h.clients, clientID)
		h.mu.Unlock()
		h.service.Unsubscribe(nil, subscriptionID)
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		// Handle ping/pong
		if msgType, ok := msg["type"].(string); ok {
			switch msgType {
			case "ping":
				conn.WriteJSON(map[string]string{"type": "pong"})
			case "unsubscribe":
				h.service.Unsubscribe(nil, subscriptionID)
				conn.WriteJSON(api.Response{
					Success: true,
					Data:    map[string]string{"status": "unsubscribed"},
				})
				return
			}
		}
	}
}

// GetTrades godoc
// @Summary      Get recent trades
// @Description  Get recent trades for a venue instrument
// @Tags         market
// @Produce      json
// @Param        venue_id      query      string  true  "Venue ID"
// @Param        instrument_id query      string  true  "Instrument ID"
// @Param        limit         query      int     false "Limit (default 100)"
// @Success      200  {object}  api.Response{data=[]TradeEvent}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/market/trades [get]
func (h *Handler) GetTrades(c *gin.Context) {
	venueIDStr := c.Query("venue_id")
	instrumentIDStr := c.Query("instrument_id")

	venueID, err := uuid.Parse(venueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid venue_id",
			},
		})
		return
	}

	instrumentID, err := uuid.Parse(instrumentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument_id",
			},
		})
		return
	}

	limit := 100
	trades, err := h.service.GetRecentTrades(c.Request.Context(), venueID, instrumentID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "INTERNAL_ERROR",
				Message: "failed to get trades",
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    trades,
	})
}

// GetTicker godoc
// @Summary      Get latest ticker
// @Description  Get latest ticker for a venue instrument
// @Tags         market
// @Produce      json
// @Param        venue_id      query      string  true  "Venue ID"
// @Param        instrument_id query      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=TickerEvent}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/market/ticker [get]
func (h *Handler) GetTicker(c *gin.Context) {
	venueIDStr := c.Query("venue_id")
	instrumentIDStr := c.Query("instrument_id")

	venueID, err := uuid.Parse(venueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid venue_id",
			},
		})
		return
	}

	instrumentID, err := uuid.Parse(instrumentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument_id",
			},
		})
		return
	}

	ticker, err := h.service.GetLatestTicker(c.Request.Context(), venueID, instrumentID)
	if err != nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "MARKET-507",
				Message: "ticker not found",
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    ticker,
	})
}

// GetFunding godoc
// @Summary      Get latest funding rate
// @Description  Get latest funding rate for a venue instrument
// @Tags         market
// @Produce      json
// @Param        venue_id      query      string  true  "Venue ID"
// @Param        instrument_id query      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=FundingEvent}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/market/funding [get]
func (h *Handler) GetFunding(c *gin.Context) {
	venueIDStr := c.Query("venue_id")
	instrumentIDStr := c.Query("instrument_id")

	venueID, err := uuid.Parse(venueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid venue_id",
			},
		})
		return
	}

	instrumentID, err := uuid.Parse(instrumentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument_id",
			},
		})
		return
	}

	funding, err := h.service.GetLatestFunding(c.Request.Context(), venueID, instrumentID)
	if err != nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "MARKET-506",
				Message: "funding rate not found",
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    funding,
	})
}

// GetSubscriptions godoc
// @Summary      Get active subscriptions
// @Description  Get all active market data subscriptions
// @Tags         market
// @Produce      json
// @Success      200  {object}  api.Response{data=[]Subscription}
// @Router       /api/v1/market/subscriptions [get]
func (h *Handler) GetSubscriptions(c *gin.Context) {
	subs := h.service.GetActiveSubscriptions()
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    subs,
	})
}
