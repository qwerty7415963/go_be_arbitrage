package unifiedstate

import (
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
)

type Handler struct {
	service   *Service
	clients   map[uuid.UUID]*websocket.Conn
	mu        sync.RWMutex
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

// GetInstruments godoc
// @Summary      Get all unified instrument states
// @Description  Get all unified instrument states across venues
// @Tags         unified
// @Produce      json
// @Success      200  {object}  api.Response{data=map[string]InstrumentState}
// @Router       /api/v1/unified/instruments [get]
func (h *Handler) GetInstruments(c *gin.Context) {
	states := h.service.GetAllInstrumentStates()
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    states,
	})
}

// GetInstrument godoc
// @Summary      Get unified instrument state
// @Description  Get unified state for a specific instrument
// @Tags         unified
// @Produce      json
// @Param        id  path      string  true  "Instrument ID"
// @Success      200  {object}  api.Response{data=InstrumentState}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/unified/instruments/{id} [get]
func (h *Handler) GetInstrument(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument id",
			},
		})
		return
	}

	state := h.service.GetInstrumentState(id)
	if state == nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "MARKET-501",
				Message: "instrument not found",
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    state,
	})
}

// GetExecutableDepth godoc
// @Summary      Get executable depth
// @Description  Get executable depth for an instrument
// @Tags         unified
// @Produce      json
// @Param        id     path      string  true  "Instrument ID"
// @Param        depth  query     int     false "Depth levels (default 10, max 20)"
// @Success      200  {object}  api.Response{data=ExecutableDepth}
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Failure      404  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/unified/instruments/{id}/depth [get]
func (h *Handler) GetExecutableDepth(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "VALIDATION_ERROR",
				Message: "invalid instrument id",
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

	depthData := h.service.GetExecutableDepth(id, depth)
	if depthData == nil {
		c.JSON(http.StatusNotFound, api.Response{
			Success: false,
			Error: &api.ErrorBody{
				Code:    "MARKET-501",
				Message: "instrument not found",
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
// @Summary      Get health overview
// @Description  Get health overview for all venues
// @Tags         unified
// @Produce      json
// @Success      200  {object}  api.Response{data=HealthOverview}
// @Router       /api/v1/unified/health [get]
func (h *Handler) GetHealth(c *gin.Context) {
	overview := h.service.GetHealthOverview()
	c.JSON(http.StatusOK, api.Response{
		Success: true,
		Data:    overview,
	})
}

// SubscribeWS godoc
// @Summary      Subscribe to unified state updates
// @Description  Subscribe to real-time unified state updates via WebSocket
// @Tags         unified
// @Produce      json
// @Param        instrument_id  query      string  true  "Instrument ID"
// @Success      101  {object}  api.Response
// @Failure      400  {object}  api.Response{error=api.ErrorBody}
// @Router       /api/v1/unified/ws [get]
func (h *Handler) SubscribeWS(c *gin.Context) {
	instrumentIDStr := c.Query("instrument_id")
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

	ch := h.service.Subscribe(instrumentID)

	conn.WriteJSON(api.Response{
		Success: true,
		Data: map[string]interface{}{
			"status":        "subscribed",
			"instrument_id": instrumentID,
		},
	})

	go h.handleWSMessages(clientID, instrumentID, ch, conn)
}

func (h *Handler) handleWSMessages(clientID, instrumentID uuid.UUID, ch chan *InstrumentState, conn *websocket.Conn) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, clientID)
		h.mu.Unlock()
		h.service.Unsubscribe(instrumentID, ch)
		conn.Close()
	}()

	for {
		select {
		case state, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(api.Response{
				Success: true,
				Data:    state,
			}); err != nil {
				return
			}
		}
	}
}
