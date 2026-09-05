package ws

import (
	"encoding/json"
	"log/slog"
)

type Handler struct {
	hub    *Hub
	logger *slog.Logger
}

func NewHandler(hub *Hub, logger *slog.Logger) *Handler {
	return &Handler{
		hub:    hub,
		logger: logger,
	}
}

func (h *Handler) HandleMessage(client *Client, data []byte) error {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	switch msg.Type {
	case "subscribe":
		if channel, ok := msg.Payload.(string); ok {
			client.Subscribe(channel)
			h.logger.Debug("client subscribed", "channel", channel)
		}

	case "unsubscribe":
		if channel, ok := msg.Payload.(string); ok {
			client.Unsubscribe(channel)
			h.logger.Debug("client unsubscribed", "channel", channel)
		}

	case "ping":
	 response, _ := json.Marshal(Message{
			Type:    "pong",
			Channel: "",
			Payload: nil,
		})
		client.send <- response
	}

	return nil
}

func (h *Handler) Broadcast(channel string, payload interface{}) {
	msg := Message{
		Type:    "message",
		Channel: channel,
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal broadcast message", "error", err)
		return
	}

	h.hub.Broadcast(data)
}

func (h *Handler) BroadcastToChannel(channel string, payload interface{}) {
	msg := Message{
		Type:    "message",
		Channel: channel,
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal broadcast message", "error", err)
		return
	}

	h.hub.mu.RLock()
	defer h.hub.mu.RUnlock()

	for client := range h.hub.clients {
		if client.IsSubscribed(channel) {
			select {
			case client.send <- data:
			default:
				h.logger.Warn("failed to send to client", "channel", channel)
			}
		}
	}
}
