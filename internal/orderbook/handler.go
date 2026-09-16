package orderbook

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
)

type Handler struct {
	service *Service
	hub     *WsHub
}

func NewHandler(service *Service, hub *WsHub) *Handler {
	return &Handler{service: service, hub: hub}
}

// GetOrderBook godoc
// @Summary      Get order book
// @Description  Get L2 order book for a venue instrument
// @Tags         orderbook
// @Produce      json
// @Param        venue_id       query      string  true  "Venue ID"
// @Param        instrument_id  query      string  true  "Instrument ID"
// @Param        depth          query      int     false "Depth levels (default 10, max 20)"
// @Success      200  {object}  api.Response{data=OrderBookDepth}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/orderbook/depth [get]
func (h *Handler) GetOrderBook(c *gin.Context) {
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

	depth := 10
	if depthStr := c.Query("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil {
			depth = d
		}
	}

	depthData, err := h.service.GetDepth(venueID, instrumentID, depth)
	if err != nil {
		status := http.StatusInternalServerError
		code := "INTERNAL_ERROR"

		switch err {
		case ErrBookNotFound:
			status = http.StatusNotFound
			code = "ORDERBOOK-301"
		case ErrInvalidDepth:
			status = http.StatusBadRequest
			code = "VALIDATION_ERROR"
		}

		c.JSON(status, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    code,
				Message: err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    depthData,
	})
}

// GetHealth godoc
// @Summary      Get order book health
// @Description  Get health status of an order book
// @Tags         orderbook
// @Produce      json
// @Param        venue_id       query      string  true  "Venue ID"
// @Param        instrument_id  query      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=OrderBookHealth}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/orderbook/health [get]
func (h *Handler) GetHealth(c *gin.Context) {
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

	health, err := h.service.GetHealth(venueID, instrumentID)
	if err != nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "ORDERBOOK-301",
				Message: err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    health,
	})
}

// RequestResync godoc
// @Summary      Request order book resync
// @Description  Request a resync of the order book
// @Tags         orderbook
// @Produce      json
// @Param        body  body      SubscribeRequest  true  "Resync request"
// @Success      200  {object}  api.Response
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/orderbook/resync [post]
func (h *Handler) RequestResync(c *gin.Context) {
	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: err.Error(),
			},
		})
		return
	}

	if err := h.service.RequestResync(req.VenueID, req.InstrumentID); err != nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "ORDERBOOK-301",
				Message: err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    map[string]string{"status": "resync requested"},
	})
}

// GetTradable godoc
// @Summary      Check if order book is tradable
// @Description  Check if the order book is healthy and tradable
// @Tags         orderbook
// @Produce      json
// @Param        venue_id       query      string  true  "Venue ID"
// @Param        instrument_id  query      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=map[string]bool}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/orderbook/tradable [get]
func (h *Handler) GetTradable(c *gin.Context) {
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

	tradable := h.service.IsTradable(venueID, instrumentID)

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    map[string]bool{"tradable": tradable},
	})
}

// SubscribeWS godoc
// @Summary      Subscribe to order book WebSocket
// @Description  Subscribe to real-time order book updates via WebSocket
// @Tags         orderbook
// @Param        venue_id       query      string  true  "Venue ID"
// @Param        instrument_id  query      string  true  "Instrument ID"
// @Param        depth          query      int     false "Depth levels (default 10, max 20)"
// @Success      101  {object}  string
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/orderbook/ws [get]
func (h *Handler) SubscribeWS(c *gin.Context) {
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

	depth := 10
	if depthStr := c.Query("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 && d <= 20 {
			depth = d
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("orderbook ws: upgrade error: %v", err)
		return
	}

	ctx := c.Request.Context()
	client := &WsClient{
		hub:          h.hub,
		conn:         conn,
		send:         make(chan []byte, 256),
		service:      h.service,
		venueID:      venueID,
		instrumentID: instrumentID,
		depth:        depth,
	}

	subCtx, cancel := context.WithCancel(ctx)
	client.cancel = cancel

	h.hub.register <- client

	book := h.service.GetBook(venueID, instrumentID)
	if book != nil {
		book.mu.RLock()
		currentSnapshot := &OrderBookSnapshot{
			VenueID:      venueID,
			InstrumentID: instrumentID,
			Sequence:     book.Sequence,
			BestBid:      getBestBid(book.Bids),
			BestAsk:      getBestAsk(book.Asks),
			Bids:         clonePriceLevels(book.Bids),
			Asks:         clonePriceLevels(book.Asks),
			Timestamp:    book.LastUpdate,
		}
		book.mu.RUnlock()
		client.sendSnapshot(currentSnapshot)
	}

	ch := h.service.Subscribe(venueID, instrumentID)

	go client.writePump()
	go client.readPump()

	go func() {
		defer func() {
			h.service.Unsubscribe(venueID, instrumentID, ch)
			h.hub.unregister <- client
		}()

		for {
			select {
			case <-subCtx.Done():
				return
			case snapshot, ok := <-ch:
				if !ok {
					return
				}
				client.sendSnapshot(snapshot)
			}
		}
	}()
}

func getBestBid(bids []PriceLevel) *string {
	if len(bids) > 0 {
		return &bids[0].Price
	}
	return nil
}

func getBestAsk(asks []PriceLevel) *string {
	if len(asks) > 0 {
		return &asks[0].Price
	}
	return nil
}

func clonePriceLevels(levels []PriceLevel) []PriceLevel {
	if levels == nil {
		return nil
	}
	clone := make([]PriceLevel, len(levels))
	copy(clone, levels)
	return clone
}
