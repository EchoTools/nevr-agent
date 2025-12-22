package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// Reconnection settings
	initialReconnectDelay = 1 * time.Second
	maxReconnectDelay     = 30 * time.Second
	reconnectBackoffMult  = 2.0
)

// WebSocketWriter implements FrameWriter and streams frames to the API server over WebSocket.
type WebSocketWriter struct {
	logger     *zap.Logger
	socketURL  string
	jwtToken   string
	ctx        context.Context
	cancel     context.CancelFunc
	conn       *websocket.Conn
	mu         sync.Mutex
	outgoingCh chan *telemetry.LobbySessionStateFrame
	stopped    bool
	connected  bool

	// Reconnection state
	reconnectCh chan struct{}
}

// NewWebSocketWriter creates a new WebSocketWriter.
func NewWebSocketWriter(logger *zap.Logger, socketURL, jwtToken string) *WebSocketWriter {
	ctx, cancel := context.WithCancel(context.Background())

	w := &WebSocketWriter{
		logger:      logger.With(zap.String("component", "websocket_writer")),
		socketURL:   socketURL,
		jwtToken:    jwtToken,
		ctx:         ctx,
		cancel:      cancel,
		outgoingCh:  make(chan *telemetry.LobbySessionStateFrame, 1000),
		stopped:     false,
		reconnectCh: make(chan struct{}, 1),
	}

	return w
}

// Connect establishes the WebSocket connection.
func (w *WebSocketWriter) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.connectLocked()
}

// connectLocked establishes the WebSocket connection (must be called with lock held)
func (w *WebSocketWriter) connectLocked() error {
	if w.connected {
		return nil
	}

	// Ensure URL scheme is correct (ws or wss)
	u, err := url.Parse(w.socketURL)
	if err != nil {
		return fmt.Errorf("invalid socket URL: %w", err)
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}

	header := http.Header{}
	if w.jwtToken != "" {
		header.Set("Authorization", "Bearer "+w.jwtToken)
	}

	w.logger.Info("Connecting to WebSocket", zap.String("url", u.String()))

	conn, _, err := websocket.DefaultDialer.DialContext(w.ctx, u.String(), header)
	if err != nil {
		return fmt.Errorf("failed to dial websocket: %w", err)
	}

	w.conn = conn
	w.connected = true

	w.logger.Debug("WebSocket connection established, starting background routines", zap.String("url", u.String()))

	// Start background routines
	go w.readLoop()
	go w.writeLoop()
	go w.reconnectLoop()

	return nil
}

// triggerReconnect signals that a reconnection is needed
func (w *WebSocketWriter) triggerReconnect() {
	select {
	case w.reconnectCh <- struct{}{}:
	default:
		// Reconnect already pending
	}
}

// reconnectLoop handles automatic reconnection with exponential backoff
func (w *WebSocketWriter) reconnectLoop() {
	delay := initialReconnectDelay

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.reconnectCh:
			// Connection lost, attempt to reconnect
			for {
				select {
				case <-w.ctx.Done():
					return
				default:
				}

				w.logger.Info("Attempting to reconnect", zap.Duration("delay", delay))
				time.Sleep(delay)

				w.mu.Lock()
				if w.stopped {
					w.mu.Unlock()
					return
				}

				// Close existing connection if any
				if w.conn != nil {
					w.conn.Close()
					w.conn = nil
				}
				w.connected = false

				err := w.connectLocked()
				w.mu.Unlock()

				if err != nil {
					w.logger.Warn("Reconnection failed", zap.Error(err), zap.Duration("next_retry", delay))
					// Exponential backoff
					delay = time.Duration(float64(delay) * reconnectBackoffMult)
					if delay > maxReconnectDelay {
						delay = maxReconnectDelay
					}
					continue
				}

				// Successfully reconnected
				w.logger.Info("Successfully reconnected to WebSocket")
				delay = initialReconnectDelay // Reset backoff
				break
			}
		}
	}
}

// Context returns the writer context.
func (w *WebSocketWriter) Context() context.Context {
	return w.ctx
}

// WriteFrame queues a frame for sending.
func (w *WebSocketWriter) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	if w.IsStopped() {
		return fmt.Errorf("writer is stopped")
	}

	select {
	case w.outgoingCh <- frame:
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	default:
		w.logger.Warn("Outgoing channel full, dropping frame")
		return fmt.Errorf("outgoing channel full")
	}
}

// Close stops the writer and closes the connection.
func (w *WebSocketWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}

	w.stopped = true
	w.cancel()

	if w.conn != nil {
		w.conn.Close()
	}
}

// IsStopped returns whether the writer is stopped.
func (w *WebSocketWriter) IsStopped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped
}

func (w *WebSocketWriter) readLoop() {
	defer func() {
		w.logger.Debug("Read loop stopped")
	}()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.mu.Lock()
		conn := w.conn
		w.mu.Unlock()

		if conn == nil {
			// No connection, wait a bit and check again
			time.Sleep(100 * time.Millisecond)
			continue
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				w.logger.Info("WebSocket closed normally")
			} else if !strings.Contains(err.Error(), "use of closed network connection") {
				w.logger.Warn("WebSocket read error, triggering reconnect", zap.Error(err))
			}

			w.mu.Lock()
			w.connected = false
			w.mu.Unlock()

			w.triggerReconnect()
			return
		}

		// Parse response (optional, mostly for acks/errors)
		var response map[string]interface{}
		if err := json.Unmarshal(message, &response); err == nil {
			if success, ok := response["success"].(bool); ok && !success {
				if errMsg, ok := response["error"].(string); ok {
					w.logger.Error("Server returned error", zap.String("error", errMsg))
				}
			}
		}
	}
}

func (w *WebSocketWriter) writeLoop() {
	ticker := time.NewTicker(50 * time.Second) // Keep-alive ping
	defer func() {
		ticker.Stop()
		w.logger.Debug("Write loop stopped")
	}()

	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		UseEnumNumbers:  true,
		EmitUnpopulated: false,
	}

	for {
		select {
		case <-w.ctx.Done():
			return

		case <-ticker.C:
			w.mu.Lock()
			conn := w.conn
			connected := w.connected
			w.mu.Unlock()

			if !connected || conn == nil {
				continue
			}

			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				w.logger.Warn("Failed to send ping, triggering reconnect", zap.Error(err))
				w.mu.Lock()
				w.connected = false
				w.mu.Unlock()
				w.triggerReconnect()
				return
			}

		case frame := <-w.outgoingCh:
			w.mu.Lock()
			conn := w.conn
			connected := w.connected
			w.mu.Unlock()

			if !connected || conn == nil {
				// Buffer the frame back if possible, otherwise drop it
				select {
				case w.outgoingCh <- frame:
				default:
					w.logger.Warn("Dropping frame while disconnected, buffer full")
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Log event count for debugging
			if len(frame.Events) > 0 {
				w.logger.Debug("Sending frame with events",
					zap.Int("event_count", len(frame.Events)),
					zap.Uint32("frame_index", frame.FrameIndex))
			}

			// Wrap frame in Envelope
			envelope := &telemetry.Envelope{
				Message: &telemetry.Envelope_Frame{
					Frame: frame,
				},
			}

			data, err := marshaler.Marshal(envelope)
			if err != nil {
				w.logger.Error("Failed to marshal envelope", zap.Error(err))
				continue
			}

			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err = conn.WriteMessage(websocket.TextMessage, data)

			if err != nil {
				w.logger.Warn("Failed to write message, triggering reconnect", zap.Error(err))
				w.mu.Lock()
				w.connected = false
				w.mu.Unlock()
				w.triggerReconnect()
				return
			}
		}
	}
}
