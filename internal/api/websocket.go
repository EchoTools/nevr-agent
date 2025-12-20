package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/amqp"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/gofrs/uuid/v5"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer (10MB)
	maxMessageSize = 10 * 1024 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now - you may want to restrict this
		return true
	},
}

// WebSocketStreamHandler handles websocket connections for streaming session events
func (s *Server) WebSocketStreamHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade connection", "error", err)
		return
	}
	defer conn.Close()

	// Extract optional node and user ID from headers
	node := r.Header.Get("X-Node-ID")
	if node == "" {
		node = "default-node"
	}
	userID := r.Header.Get("X-User-ID")

	s.logger.Info("WebSocket connection established", "remote_addr", r.RemoteAddr, "node", node, "user_id", userID)

	// Configure connection
	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start ping ticker to keep connection alive
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	// Create channels for message handling
	messageChan := make(chan []byte, 10)
	errorChan := make(chan error, 1)
	done := make(chan struct{})

	// Start reader goroutine
	go s.readWebSocketMessages(conn, messageChan, errorChan, done)

	// Start writer/ping goroutine
	go s.writeWebSocketPings(conn, ticker, done)

	// Main message processing loop
	ctx := r.Context()
	for {
		select {
		case message := <-messageChan:
			if err := s.processWebSocketMessage(ctx, message, node, userID); err != nil {
				s.logger.Error("Failed to process message", "error", err)
				// Send error back to client
				if err := s.sendWebSocketError(conn, err); err != nil {
					s.logger.Error("Failed to send error", "error", err)
					return
				}
			} else {
				// Send success acknowledgment
				if err := s.sendWebSocketAck(conn); err != nil {
					s.logger.Error("Failed to send acknowledgment", "error", err)
					return
				}
			}

		case err := <-errorChan:
			s.logger.Error("WebSocket error", "error", err)
			return

		case <-done:
			s.logger.Info("WebSocket connection closed")
			return

		case <-ctx.Done():
			s.logger.Info("Context cancelled, closing connection")
			return
		}
	}
}

// readWebSocketMessages reads messages from the websocket connection
func (s *Server) readWebSocketMessages(conn *websocket.Conn, messageChan chan<- []byte, errorChan chan<- error, done chan<- struct{}) {
	defer close(done)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				errorChan <- err
			}
			return
		}

		messageChan <- message
	}
}

// writeWebSocketPings sends periodic ping messages
func (s *Server) writeWebSocketPings(conn *websocket.Conn, ticker *time.Ticker, done <-chan struct{}) {
	for {
		select {
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// processWebSocketMessage processes a single message from the websocket
func (s *Server) processWebSocketMessage(ctx context.Context, message []byte, node, userID string) error {
	// Parse the payload as Envelope
	msg := &telemetry.Envelope{}
	if err := protojson.Unmarshal(message, msg); err != nil {
		return fmt.Errorf("invalid protobuf payload: %w", err)
	}

	// Ignore messages that are not LobbySessionStateFrame
	if msg.GetFrame() == nil || msg.GetFrame().GetSession() == nil {
		return nil
	}

	frame := msg.GetFrame()
	lobbySessionID := frame.GetSession().GetSessionId()

	matchID := MatchID{
		UUID: uuid.FromStringOrNil(lobbySessionID),
		Node: node,
	}

	if !matchID.IsValid() {
		return fmt.Errorf("invalid match ID: %s", lobbySessionID)
	}

	// Store the frame to MongoDB
	if err := StoreSessionFrame(ctx, s.mongoClient, lobbySessionID, userID, frame); err != nil {
		return fmt.Errorf("failed to store session frame: %w", err)
	}

	// Publish to AMQP if publisher is available
	if s.amqpPublisher != nil && s.amqpPublisher.IsConnected() {
		amqpEvent := &amqp.MatchEvent{
			Type:           "session.frame",
			LobbySessionID: lobbySessionID,
			UserID:         userID,
			Timestamp:      frame.Timestamp.AsTime(),
		}
		if err := s.amqpPublisher.Publish(ctx, amqpEvent); err != nil {
			// Log error but don't fail - AMQP is best-effort
			s.logger.Warn("Failed to publish AMQP event", "error", err)
		}
	}

	return nil
}

// sendWebSocketError sends an error message to the client
func (s *Server) sendWebSocketError(conn *websocket.Conn, err error) error {
	response := map[string]interface{}{
		"success": false,
		"error":   err.Error(),
	}
	return s.sendWebSocketJSON(conn, response)
}

// sendWebSocketAck sends a success acknowledgment to the client
func (s *Server) sendWebSocketAck(conn *websocket.Conn) error {
	response := map[string]interface{}{
		"success": true,
	}
	return s.sendWebSocketJSON(conn, response)
}

// sendWebSocketJSON sends a JSON message to the websocket client
func (s *Server) sendWebSocketJSON(conn *websocket.Conn, v interface{}) error {
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteJSON(v)
}

// StreamResponse represents a response sent over the websocket
type StreamResponse struct {
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	LobbySessionID string `json:"lobby_session_id,omitempty"`
}
