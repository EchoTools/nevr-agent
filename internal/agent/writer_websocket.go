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
}

// NewWebSocketWriter creates a new WebSocketWriter.
func NewWebSocketWriter(logger *zap.Logger, socketURL, jwtToken string) *WebSocketWriter {
	ctx, cancel := context.WithCancel(context.Background())

	w := &WebSocketWriter{
		logger:     logger.With(zap.String("component", "websocket_writer")),
		socketURL:  socketURL,
		jwtToken:   jwtToken,
		ctx:        ctx,
		cancel:     cancel,
		outgoingCh: make(chan *telemetry.LobbySessionStateFrame, 1000),
		stopped:    false,
	}

	return w
}

// Connect establishes the WebSocket connection.
func (w *WebSocketWriter) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.connected {
		return nil
	}

	// Ensure URL scheme is correct (ws or wss)
	u, err := url.Parse(w.socketURL)
	if err != nil {
		return fmt.Errorf("invalid socket URL: %w", err)
	}

	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
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

	// Start background routines
	go w.readLoop()
	go w.writeLoop()

	return nil
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
		w.logger.Info("Read loop stopped")
		w.Close()
	}()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		_, message, err := w.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) && !strings.Contains(err.Error(), "use of closed network connection") {
				w.logger.Error("WebSocket read error", zap.Error(err))
			}
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
		w.logger.Info("Write loop stopped")
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
			if err := w.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				w.logger.Error("Failed to send ping", zap.Error(err))
				w.mu.Unlock()
				return
			}
			w.mu.Unlock()

		case frame := <-w.outgoingCh:
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

			w.mu.Lock()
			w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err = w.conn.WriteMessage(websocket.TextMessage, data)
			w.mu.Unlock()

			if err != nil {
				w.logger.Error("Failed to write message", zap.Error(err))
				return
			}
		}
	}
}
