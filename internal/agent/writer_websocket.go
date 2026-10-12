package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/conversion"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	logger    *zap.Logger
	socketURL string
	jwtToken  string
	ctx       context.Context
	cancel    context.CancelFunc
	conn      *websocket.Conn
	// connCancel ends the loops (readLoop, writeLoop) started for the CURRENT
	// conn. Every connection gets its own, so replacing a conn retires exactly
	// the goroutines that belonged to it.
	connCancel context.CancelFunc
	mu         sync.Mutex
	outgoingCh chan *telemetry.LobbySessionStateFrame
	stopped    bool
	connected  bool
	// supervising guards the single reconnectLoop; see Connect.
	supervising bool

	// writeMu serialises EVERY conn.Write* in this file. gorilla/websocket
	// permits one concurrent writer and panics ("concurrent write to websocket
	// connection") otherwise, killing the process. Three writers can reach one
	// connection here: the ping ticker, the frame path, and the footer written
	// from Close().
	writeMu sync.Mutex

	// Reconnection state
	reconnectCh    chan struct{}
	disconnectedAt time.Time

	// v2 capture state. The wire is telemetry v2 (see writeEnvelope): a
	// CaptureHeader, then Frames, then a CaptureFooter. The server builds a fresh
	// session per CONNECTION and rejects a frame that arrives before a header, so
	// the header must be re-sent after every reconnect -- not once per process.
	//
	// That is tracked by epoch rather than by a bool reset at each disconnect
	// site: connEpoch increments on every successful connect (one write site),
	// headerEpoch records the epoch the header was last accepted in. They differ
	// exactly when this connection has not been given a header yet, which is
	// correct for disconnect paths nobody enumerated.
	connEpoch   uint64
	headerEpoch uint64
	mapper      *conversion.FrameMapper
	header      *capturepb.CaptureHeader
	frameCount  uint32
	lastTsMs    uint32
	// captureSessionID is the game `sessionid` the current header and mapper are
	// for. A frame arriving with a DIFFERENT one is a new match begun on the same
	// connection -- back-to-back matches with no >5s /session gap, so the poller
	// never timed out and this writer was never re-created. See sendFrame.
	captureSessionID string

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

// Connect establishes the WebSocket connection and starts the single supervisor.
//
// reconnectLoop is started HERE, exactly once, rather than in connectLocked.
// connectLocked is what reconnectLoop calls to re-dial, so starting a supervisor
// from inside it meant every reconnect spawned another supervisor, each of which
// spawned another on its next reconnect -- geometric growth, and with it multiple
// readLoops calling ReadMessage on one connection. gorilla permits ONE concurrent
// reader and one concurrent writer; the race detector reports the read side as
// ten DATA RACEs inside Conn.NextReader across a single reconnect test, and the
// write side is what writeMu now covers.
//
// This is the minimum fix for that: one supervisor, so nothing multiplies. The
// per-connection loops still respawn, because they must -- they return when their
// own connection dies.
func (w *WebSocketWriter) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.connectLocked(); err != nil {
		return err
	}
	if !w.supervising {
		w.supervising = true
		go w.reconnectLoop()
	}
	return nil
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

	connCtx, connCancel := context.WithCancel(w.ctx)
	w.conn = conn
	w.connCancel = connCancel
	w.connected = true
	w.connEpoch++

	w.logger.Debug("WebSocket connection established, starting background routines", zap.String("url", u.String()))

	// Start the per-connection routines. Both are bound to THIS conn: they act
	// only while it is still w.conn, and exit when connCtx is cancelled (the next
	// reconnect or Close). A loop that outlived its conn must never mark its
	// successor disconnected or trigger a reconnect. reconnectLoop is NOT
	// started here -- see Connect.
	go w.readLoop(conn, connCtx)
	go w.writeLoop(conn, connCtx)

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

// connLost records that conn failed and asks for a reconnect -- but only if conn
// is still the current connection. A loop belonging to a connection that has
// already been replaced reports nothing: its failure says nothing about the
// healthy connection that superseded it.
func (w *WebSocketWriter) connLost(conn *websocket.Conn) {
	w.mu.Lock()
	if w.conn != conn {
		w.mu.Unlock()
		return
	}
	w.connected = false
	w.disconnectedAt = time.Now()
	w.mu.Unlock()
	w.triggerReconnect()
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

				// Retire the old connection and its loops. Cancel first so the
				// old readLoop, woken by the Close, sees it is superseded.
				if w.connCancel != nil {
					w.connCancel()
					w.connCancel = nil
				}
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
	// Close the capture before tearing the connection down. Taken outside the
	// lock because sendFooter takes it.
	w.sendFooter()

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

func (w *WebSocketWriter) readLoop(conn *websocket.Conn, connCtx context.Context) {
	defer func() {
		w.logger.Debug("Read loop stopped")
	}()

	for {
		select {
		case <-connCtx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-connCtx.Done():
				return // superseded or closing: this error is ours, not news
			default:
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				w.logger.Info("WebSocket closed normally")
			} else if !strings.Contains(err.Error(), "use of closed network connection") {
				w.logger.Warn("WebSocket read error, triggering reconnect", zap.Error(err))
			}

			w.connLost(conn)
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

func (w *WebSocketWriter) writeLoop(conn *websocket.Conn, connCtx context.Context) {
	ticker := time.NewTicker(50 * time.Second) // Keep-alive ping
	defer func() {
		ticker.Stop()
		if w.ctx.Err() != nil {
			// Only on shutdown: a superseded writeLoop must not delete the
			// disk buffer the new connection is about to drain.
			w.cleanupDiskBuffer()
		}
		w.logger.Debug("Write loop stopped")
	}()

	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		UseEnumNumbers:  true,
		EmitUnpopulated: false,
	}

	for {
		select {
		case <-connCtx.Done():
			return

		case <-ticker.C:
			w.mu.Lock()
			current := w.conn == conn
			connected := w.connected
			w.mu.Unlock()

			if !connected || !current {
				continue
			}

			w.writeMu.Lock()
			err := conn.WriteMessage(websocket.PingMessage, nil)
			w.writeMu.Unlock()
			if err != nil {
				w.logger.Warn("Failed to send ping, triggering reconnect", zap.Error(err))
				w.connLost(conn)
				return
			}

		case frame := <-w.outgoingCh:
			w.mu.Lock()
			current := w.conn == conn
			connected := w.connected
			disconnectedAt := w.disconnectedAt
			w.mu.Unlock()

			if !current {
				// Superseded between the select and here: hand the frame to
				// the loop that owns the live connection.
				select {
				case w.outgoingCh <- frame:
				default:
					w.logger.Warn("Dropping frame while superseded, buffer full")
				}
				return
			}

			if !connected {
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

			if err := w.sendFrame(conn, frame); err != nil {
				w.logger.Warn("Failed to write message, triggering reconnect", zap.Error(err))
				w.connLost(conn)
				return
			}
		}
	}
}

// encodeEnvelope serialises a telemetry v2 Envelope in the ingest wire format:
// a 4-byte little-endian length prefix followed by the marshalled Envelope. This
// is the exact framing nevr-stream's ParseEnvelope expects, and the same framing
// its reflected stream emits back, so one codec serves both directions.
func encodeEnvelope(env *capturepb.Envelope) ([]byte, error) {
	data, err := proto.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf, nil
}

// writeEnvelope writes one length-prefixed v2 Envelope as a BINARY message.
// The previous wire was a telemetry v1 Envelope marshalled with protojson and
// sent as a TEXT message; nevr-stream accepts neither -- it parses a 4-byte LE
// prefix into a telemetry v2 Envelope and rejects any non-binary frame outright
// ("only binary messages accepted"). Both halves were built and could not talk.
func (w *WebSocketWriter) writeEnvelope(conn *websocket.Conn, env *capturepb.Envelope) error {
	data, err := encodeEnvelope(env)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// sendFrame maps a v1 frame to v2 and writes it, sending the CaptureHeader first
// whenever the current connection has not been given one. Mapping is the same
// path writer_tape.go uses (MapHeaderFromSession + FrameMapper), so the socket
// and the .tape file carry identical captures rather than two dialects.
func (w *WebSocketWriter) sendFrame(conn *websocket.Conn, frame *telemetry.LobbySessionStateFrame) error {
	// Detect a new match on the SAME connection before anything else. A real
	// gameserver serving match B right after match A, with no /session gap long
	// enough to time out the poller, keeps this one writer alive across both --
	// so without this, B's frames ride A's header and nevr-stream attributes
	// them to A's match_id, which never gets a footer (measured 2026-08-28).
	sid := frame.GetSession().GetSessionId()
	w.mu.Lock()
	oldSID := w.captureSessionID
	sessionChanged := w.mapper != nil && sid != "" && oldSID != "" && sid != oldSID
	w.mu.Unlock()
	if sessionChanged {
		w.logger.Info("session changed on a live connection; closing the old capture and starting a new one",
			zap.String("old_session", oldSID), zap.String("new_session", sid))
		w.sendFooter() // footers the OLD capture from its frameCount/lastTsMs
		w.mu.Lock()
		w.mapper = nil
		w.header = nil
		w.frameCount = 0
		w.lastTsMs = 0
		w.headerEpoch = 0 // force a header for the new capture (connEpoch >= 1 once connected)
		w.captureSessionID = ""
		w.mu.Unlock()
	}

	w.mu.Lock()
	needHeader := w.headerEpoch != w.connEpoch
	epoch := w.connEpoch
	mapper := w.mapper
	w.mu.Unlock()

	if mapper == nil {
		// First frame of the capture: its session data is what the header is
		// built from, and its timestamp is the base every offset is relative to.
		base := frame.GetTimestamp().AsTime()
		v1hdr := &telemetry.TelemetryHeader{
			CaptureId: frame.GetSession().GetSessionId(),
			CreatedAt: timestamppb.New(base),
			Metadata:  map[string]string{"format": "tape"},
		}
		header := conversion.MapHeaderFromSession(v1hdr, frame.GetSession())
		mapper = &conversion.FrameMapper{BaseTime: base}

		w.mu.Lock()
		w.header = header
		w.mapper = mapper
		w.captureSessionID = frame.GetSession().GetSessionId()
		w.mu.Unlock()
	}

	if needHeader {
		w.mu.Lock()
		header := w.header
		w.mu.Unlock()
		if header == nil {
			return fmt.Errorf("no capture header available")
		}
		if err := w.writeEnvelope(conn, capturepb.Envelope_builder{Header: header}.Build()); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
		w.mu.Lock()
		// Guard against a reconnect that raced this write: only claim the epoch
		// this header was actually written for.
		if w.connEpoch == epoch {
			w.headerEpoch = epoch
		}
		w.mu.Unlock()
		w.logger.Debug("Sent capture header", zap.Uint64("conn_epoch", epoch))

		// A new connection is a new session on the server -- a fresh tape file
		// and fresh spectators -- so the mapper's delta state must start over:
		// tape's FrameMapper emits PlayerStatsUpdated, LoadoutChanged and
		// GrabChanged on FIRST SIGHTING and then only on change. Carried across a
		// reconnect it sends a header and then frames with no baseline, and a
		// server restarted mid-match shows every spectator names and no numbers
		// until the next match (measured on the rig, 2026-08-28). RoundNumber is
		// match state rather than delta state and survives the reset.
		rn := mapper.RoundNumber
		mapper.Reset()
		mapper.RoundNumber = rn
	}

	v2frame, err := mapper.MapFrame(frame)
	if err != nil {
		// A frame that cannot be mapped is dropped rather than killing the
		// connection: the error is per-frame (timestamp offset overflow), not
		// per-stream, and taking the socket down would lose the whole capture.
		w.logger.Error("Failed to map frame to v2, dropping it", zap.Error(err),
			zap.Uint32("frame_index", frame.GetFrameIndex()))
		return nil
	}

	if err := w.writeEnvelope(conn, capturepb.Envelope_builder{Frame: v2frame}.Build()); err != nil {
		return err
	}

	w.mu.Lock()
	w.frameCount++
	if ts := v2frame.GetTimestampOffsetMs(); ts > w.lastTsMs {
		w.lastTsMs = ts
	}
	w.mu.Unlock()
	return nil
}

// sendFooter writes the CaptureFooter that closes the capture. A capture that
// ends without one is a truncated capture, which is exactly what a dropped
// connection should look like -- so this is best-effort and never synthesised
// from a connection that never carried a header.
func (w *WebSocketWriter) sendFooter() {
	w.mu.Lock()
	conn := w.conn
	connected := w.connected && !w.stopped
	sent := w.headerEpoch == w.connEpoch && w.headerEpoch != 0
	footer := capturepb.CaptureFooter_builder{
		FrameCount: w.frameCount,
		DurationMs: w.lastTsMs,
	}.Build()
	w.mu.Unlock()

	if !connected || conn == nil || !sent {
		return
	}
	if err := w.writeEnvelope(conn, capturepb.Envelope_builder{Footer: footer}.Build()); err != nil {
		w.logger.Warn("Failed to send capture footer", zap.Error(err))
		return
	}
	w.logger.Info("Sent capture footer",
		zap.Uint32("frames", footer.GetFrameCount()),
		zap.Uint32("duration_ms", footer.GetDurationMs()))
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

		// Buffered lines are v1 protojson Envelopes (see bufferToDisk). They are
		// re-mapped and sent through sendFrame rather than replayed raw: the raw
		// bytes are the OLD wire, so draining them verbatim would push text
		// frames the server rejects -- and only after a reconnect, which is the
		// hardest case to notice. Routing them through sendFrame also means a
		// drained frame re-sends the CaptureHeader when the new connection needs
		// one, exactly like a live frame.
		var buffered telemetry.Envelope
		if err := protojson.Unmarshal(scanner.Bytes(), &buffered); err != nil {
			w.logger.Warn("Skipping unparseable buffered frame", zap.Error(err))
			continue
		}
		bf := buffered.GetFrame()
		if bf == nil {
			continue
		}
		if err := w.sendFrame(conn, bf); err != nil {
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
