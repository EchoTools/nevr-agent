package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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

	// Buffer settings
	memoryBufferSize     = 1000                  // Max frames to keep in memory
	diskBufferThreshold  = 3 * time.Second       // Start disk buffering after this duration
	catchUpBatchSize     = 100                   // Frames to send per batch when catching up
	catchUpBatchInterval = 10 * time.Millisecond // Delay between catch-up batches
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
	reconnectCh    chan struct{}
	disconnectedAt time.Time

	// Disk buffer state
	diskBufferMu    sync.Mutex
	diskBufferFile  *os.File
	diskBufferPath  string
	usingDiskBuffer bool
	diskFrameCount  int64
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
		outgoingCh:  make(chan *telemetry.LobbySessionStateFrame, memoryBufferSize),
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

				// Drain any buffered frames from disk
				go w.drainDiskBuffer()
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

	// Clean up disk buffer
	go w.cleanupDiskBuffer()
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
		w.cleanupDiskBuffer()
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
				w.disconnectedAt = time.Now()
				w.mu.Unlock()
				w.triggerReconnect()
				return
			}

		case frame := <-w.outgoingCh:
			w.mu.Lock()
			conn := w.conn
			connected := w.connected
			disconnectedAt := w.disconnectedAt
			w.mu.Unlock()

			if !connected || conn == nil {
				// Check if we should switch to disk buffering
				if !disconnectedAt.IsZero() && time.Since(disconnectedAt) > diskBufferThreshold {
					if err := w.bufferToDisk(frame, &marshaler); err != nil {
						w.logger.Warn("Failed to buffer frame to disk", zap.Error(err))
					}
				} else {
					// Still in memory buffer phase, re-queue the frame
					select {
					case w.outgoingCh <- frame:
					default:
						w.logger.Warn("Dropping frame while disconnected, buffer full")
					}
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
				w.disconnectedAt = time.Now()
				w.mu.Unlock()
				w.triggerReconnect()
				return
			}
		}
	}
}

// bufferToDisk writes a frame to the disk buffer file
func (w *WebSocketWriter) bufferToDisk(frame *telemetry.LobbySessionStateFrame, marshaler *protojson.MarshalOptions) error {
	w.diskBufferMu.Lock()
	defer w.diskBufferMu.Unlock()

	// Create disk buffer file if not exists
	if w.diskBufferFile == nil {
		f, err := os.CreateTemp("", "nevr-frame-buffer-*.jsonl")
		if err != nil {
			return fmt.Errorf("failed to create disk buffer file: %w", err)
		}
		w.diskBufferFile = f
		w.diskBufferPath = f.Name()
		w.usingDiskBuffer = true
		w.diskFrameCount = 0
		w.logger.Info("Started disk buffering", zap.String("path", w.diskBufferPath))
	}

	// Wrap frame in Envelope and marshal
	envelope := &telemetry.Envelope{
		Message: &telemetry.Envelope_Frame{
			Frame: frame,
		},
	}

	data, err := marshaler.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// Write as a line (JSONL format)
	if _, err := w.diskBufferFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to disk buffer: %w", err)
	}
	if _, err := w.diskBufferFile.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline to disk buffer: %w", err)
	}

	w.diskFrameCount++
	if w.diskFrameCount%100 == 0 {
		w.logger.Debug("Disk buffer progress", zap.Int64("frames_buffered", w.diskFrameCount))
	}

	return nil
}

// drainDiskBuffer reads and sends all buffered frames after reconnection
func (w *WebSocketWriter) drainDiskBuffer() {
	w.diskBufferMu.Lock()
	if !w.usingDiskBuffer || w.diskBufferFile == nil {
		w.diskBufferMu.Unlock()
		return
	}

	// Close the write handle and reopen for reading
	w.diskBufferFile.Close()
	path := w.diskBufferPath
	frameCount := w.diskFrameCount
	w.diskBufferMu.Unlock()

	w.logger.Info("Draining disk buffer", zap.String("path", path), zap.Int64("frames", frameCount))

	f, err := os.Open(path)
	if err != nil {
		w.logger.Error("Failed to open disk buffer for reading", zap.Error(err))
		w.cleanupDiskBuffer()
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially large JSON lines
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	sentCount := int64(0)
	batchCount := 0

	for scanner.Scan() {
		select {
		case <-w.ctx.Done():
			w.logger.Warn("Context cancelled during disk buffer drain")
			w.cleanupDiskBuffer()
			return
		default:
		}

		w.mu.Lock()
		conn := w.conn
		connected := w.connected
		w.mu.Unlock()

		if !connected || conn == nil {
			w.logger.Warn("Connection lost during disk buffer drain, aborting")
			// Don't clean up - keep the buffer for next reconnect
			return
		}

		data := scanner.Bytes()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			w.logger.Warn("Failed to send buffered frame, will retry on next reconnect", zap.Error(err))
			w.mu.Lock()
			w.connected = false
			w.disconnectedAt = time.Now()
			w.mu.Unlock()
			w.triggerReconnect()
			return
		}

		sentCount++
		batchCount++

		// Throttle to avoid overwhelming the server
		if batchCount >= catchUpBatchSize {
			batchCount = 0
			time.Sleep(catchUpBatchInterval)
		}
	}

	if err := scanner.Err(); err != nil {
		w.logger.Error("Error reading disk buffer", zap.Error(err))
	}

	w.logger.Info("Disk buffer drained successfully", zap.Int64("frames_sent", sentCount))
	w.cleanupDiskBuffer()
}

// cleanupDiskBuffer removes the disk buffer file and resets state
func (w *WebSocketWriter) cleanupDiskBuffer() {
	w.diskBufferMu.Lock()
	defer w.diskBufferMu.Unlock()

	if w.diskBufferFile != nil {
		w.diskBufferFile.Close()
		w.diskBufferFile = nil
	}

	if w.diskBufferPath != "" {
		if err := os.Remove(w.diskBufferPath); err != nil && !os.IsNotExist(err) {
			w.logger.Warn("Failed to remove disk buffer file", zap.Error(err), zap.String("path", w.diskBufferPath))
		} else if err == nil {
			w.logger.Debug("Cleaned up disk buffer file", zap.String("path", w.diskBufferPath))
		}
		w.diskBufferPath = ""
	}

	w.usingDiskBuffer = false
	w.diskFrameCount = 0
}
