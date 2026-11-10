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
	dropLogInterval      = 30 * time.Second      // Min gap between "frames dropped" log lines
	diskSizeLogInterval  = time.Minute           // Gap between disk buffer size log lines
)

// maxDiskBufferBytes caps the disk buffer. At the cap further frames are NOT
// written: they are dropped (oldest kept, so the replay stays contiguous from the
// start of the outage) and counted/logged through noteDrop. A multi-hour outage
// therefore cannot fill the temp disk unseen. The size is also logged once a
// minute while buffering. A var so tests can lower it.
var maxDiskBufferBytes int64 = 512 << 20

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
	// dialNow makes the supervisor's next round dial immediately instead of
	// sleeping the backoff first (set by ConnectWithRetry for the first dial).
	dialNow bool
	// supervisorDone is closed when reconnectLoop returns.
	supervisorDone chan struct{}

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

	// Drop accounting: see noteDrop.
	dropMu      sync.Mutex
	dropped     uint64
	lastDropLog time.Time

	// Disk buffer state
	diskBufferMu    sync.Mutex
	diskBufferFile  *os.File
	diskBufferPath  string
	usingDiskBuffer bool
	diskFrameCount  int64
	diskBytes       int64     // bytes in the current buffer file
	diskSizeLogged  time.Time // last periodic size log
	// drainConsumed is how many lines of the current buffer file earlier drains
	// already sent (or skipped as unparseable). A drain aborted mid-file resumes
	// after them, so the next server-side piece is exactly k+1..N instead of
	// re-sending 1..k (nevr-stream would store the match as two overlapping tapes).
	drainConsumed int64

	// replayHook, when set (tests), is called after each replayed frame is sent.
	replayHook func(consumed int64, conn *websocket.Conn)
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

		supervisorDone: make(chan struct{}),
	}

	return w
}

// Connect dials once, synchronously, and starts the single supervisor.
//
// reconnectLoop is started HERE (or by ConnectWithRetry), exactly once, rather
// than when a connection is installed. reconnectLoop is what re-dials, so
// starting a supervisor from inside it meant every reconnect spawned another
// supervisor, each of which spawned another on its next reconnect -- geometric
// growth, and with it multiple readLoops calling ReadMessage on one connection.
// gorilla permits ONE concurrent reader and one concurrent writer; the write
// side is what writeMu covers. The per-connection loops still respawn, because
// they must -- they return when their own connection dies.
//
// The dial happens OUTSIDE w.mu: w.mu is taken by IsStopped, which is on every
// WriteFrame, and a dial can take the full 45 s handshake timeout.
func (w *WebSocketWriter) Connect() error {
	w.mu.Lock()
	if w.connected {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	conn, err := w.dial()
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		conn.Close()
		return fmt.Errorf("writer is stopped")
	}
	connCtx := w.installLocked(conn)
	w.startLoops(conn, connCtx)
	if !w.supervising {
		w.supervising = true
		go w.reconnectLoop()
	}
	return nil
}

// ConnectWithRetry is Connect for the start of a match: a refused or blackholed
// first dial is not final, and it never blocks the caller. It starts the same
// reconnectLoop that recovers a dropped connection (same backoff, same
// supervisor -- one retry implementation, not two) with its first dial
// immediate. It returns an error only for a URL that can never dial.
//
// Drop policy: NO frame is dropped before the first connect, or while
// reconnecting. From the moment the supervisor starts a round until the
// connection is up, a spool goroutine moves frames from outgoingCh to the disk
// buffer (the same one a dropped connection uses); on connect the buffer is
// replayed, in order, before the live loops start, so an outage longer than
// memoryBufferSize frames is bounded by disk, not by 1000 frames. A frame can
// still be dropped only if the disk buffer cannot be written, or outgoingCh
// fills faster than the spool or the replay drain it; every such drop goes
// through noteDrop (logged on the first, then periodically, with a count).
//
// The spool is capped at maxDiskBufferBytes (size logged every minute; at the
// cap frames are dropped and counted). Close deletes a spool that was never
// replayed: the file writers (tape/nevrcap) still hold those frames, so the
// spool is a best-effort live-stream backlog, not the record of the match.
func (w *WebSocketWriter) ConnectWithRetry() error {
	if _, err := url.Parse(w.socketURL); err != nil {
		return fmt.Errorf("invalid socket URL: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.dialNow = true
	w.disconnectedAt = time.Now()
	if !w.supervising {
		w.supervising = true
		go w.reconnectLoop()
	}
	w.triggerReconnect()
	return nil
}

// dial opens the WebSocket. It takes no lock and is cancelled by Close.
func (w *WebSocketWriter) dial() (*websocket.Conn, error) {
	// Ensure URL scheme is correct (ws or wss)
	u, err := url.Parse(w.socketURL)
	if err != nil {
		return nil, fmt.Errorf("invalid socket URL: %w", err)
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
		return nil, fmt.Errorf("failed to dial websocket: %w", err)
	}
	return conn, nil
}

// installLocked makes conn the current connection (w.mu held). It does not
// start the per-connection loops; see startLoops.
func (w *WebSocketWriter) installLocked(conn *websocket.Conn) context.Context {
	connCtx, connCancel := context.WithCancel(w.ctx)
	w.conn = conn
	w.connCancel = connCancel
	w.connected = true
	w.connEpoch++
	return connCtx
}

// startLoops starts the per-connection routines. Both are bound to THIS conn:
// they act only while it is still w.conn, and exit when connCtx is cancelled
// (the next reconnect or Close). A loop that outlived its conn must never mark
// its successor disconnected or trigger a reconnect.
func (w *WebSocketWriter) startLoops(conn *websocket.Conn, connCtx context.Context) {
	go w.readLoop(conn, connCtx)
	go w.writeLoop(conn, connCtx)
}

// retireLocked ends the current connection and its loops (w.mu held). Cancel
// first so the old readLoop, woken by the Close, sees it is superseded.
func (w *WebSocketWriter) retireLocked() {
	if w.connCancel != nil {
		w.connCancel()
		w.connCancel = nil
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connected = false
}

// noteDrop counts a dropped frame and logs the first one, then at most once per
// dropLogInterval with the running total -- never per frame. The logger carries
// the session id (agent.go gives the writer the session-scoped logger).
func (w *WebSocketWriter) noteDrop(reason string) { w.noteDropN(1, reason) }

// noteDropN counts k dropped frames at once (e.g. the rest of a spool that
// cannot be read).
func (w *WebSocketWriter) noteDropN(k uint64, reason string) {
	if k == 0 {
		return
	}
	w.dropMu.Lock()
	first := w.dropped == 0
	w.dropped += k
	n := w.dropped
	logIt := first || time.Since(w.lastDropLog) >= dropLogInterval
	if logIt {
		w.lastDropLog = time.Now()
	}
	w.dropMu.Unlock()
	if logIt {
		w.logger.Warn("Dropping frames", zap.String("reason", reason), zap.Uint64("dropped_total", n))
	}
}

// startSpool moves frames from outgoingCh to the disk buffer until the returned
// stop function is called (which waits for it to finish, so every frame still
// in the channel afterwards is newer than every frame on disk).
func (w *WebSocketWriter) startSpool() (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		marshaler := protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: true}
		for {
			select {
			case <-w.ctx.Done():
				return
			case <-quit:
				return
			case frame := <-w.outgoingCh:
				if err := w.bufferToDisk(frame, &marshaler); err != nil {
					w.noteDrop("disk buffer write failed: " + err.Error())
				}
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
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
	if w.conn != conn || !w.connected {
		// Superseded, or already reported lost: the readLoop and writeLoop of
		// one dead conn both land here, and only the first may queue a
		// reconnect. A second signal would outlive the reconnect it duplicates
		// and tear down the healthy replacement.
		w.mu.Unlock()
		return
	}
	w.connected = false
	w.disconnectedAt = time.Now()
	w.mu.Unlock()
	w.triggerReconnect()
}

// reconnectLoop handles automatic reconnection with exponential backoff. Each
// round retires the dead connection, spools frames to disk while it dials (see
// ConnectWithRetry), and on success replays the spool before the live loops
// start. Nothing here holds w.mu across a dial or a sleep.
func (w *WebSocketWriter) reconnectLoop() {
	defer close(w.supervisorDone)
	delay := initialReconnectDelay

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.reconnectCh:
		}

		w.mu.Lock()
		if w.stopped {
			w.mu.Unlock()
			return
		}
		immediate := w.dialNow
		w.dialNow = false
		w.retireLocked()
		w.mu.Unlock()

		stopSpool := w.startSpool()

		for {
			if !immediate {
				w.logger.Info("Attempting to reconnect", zap.Duration("delay", delay))
				timer := time.NewTimer(delay)
				select {
				case <-w.ctx.Done():
					timer.Stop()
					stopSpool()
					return
				case <-timer.C:
				}
			}
			immediate = false

			conn, err := w.dial()
			if err != nil {
				if w.ctx.Err() != nil {
					stopSpool()
					return
				}
				w.logger.Warn("Reconnection failed", zap.Error(err), zap.Duration("next_retry", delay))
				// Exponential backoff
				delay = time.Duration(float64(delay) * reconnectBackoffMult)
				if delay > maxReconnectDelay {
					delay = maxReconnectDelay
				}
				continue
			}

			stopSpool()
			w.mu.Lock()
			if w.stopped {
				w.mu.Unlock()
				conn.Close()
				return
			}
			connCtx := w.installLocked(conn)
			w.mu.Unlock()

			w.logger.Info("Connected to WebSocket")
			delay = initialReconnectDelay // Reset backoff

			// Replay what was spooled, in order, before any live frame is sent.
			w.drainDiskBuffer()

			w.mu.Lock()
			if w.conn == conn && w.connected {
				w.startLoops(conn, connCtx)
			}
			w.mu.Unlock()
			break
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
		w.noteDrop("outgoing channel full")
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
					w.noteDrop("superseded, buffer full")
				}
				return
			}

			if !connected {
				// Check if we should switch to disk buffering
				if !disconnectedAt.IsZero() && time.Since(disconnectedAt) > diskBufferThreshold {
					if err := w.bufferToDisk(frame, &marshaler); err != nil {
						w.noteDrop("disk buffer write failed: " + err.Error())
					}
				} else {
					// Still in memory buffer phase, re-queue the frame
					select {
					case w.outgoingCh <- frame:
					default:
						w.noteDrop("disconnected, buffer full")
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
				// Don't lose the frame in hand: spool it BEFORE connLost, so it sits
				// ahead of anything the reconnect round spools after it, and the
				// replay re-sends it. A failed spool write is a counted drop.
				if serr := w.bufferToDisk(frame, &marshaler); serr != nil {
					w.noteDrop("in-flight frame, disk buffer write failed: " + serr.Error())
				}
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
			// A header opens a new piece on the server; its footer must count
			// its own frames, not the previous connection's.
			w.frameCount = 0
			w.lastTsMs = 0
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
		w.logger.Debug("Failed to map frame to v2", zap.Error(err),
			zap.Uint32("frame_index", frame.GetFrameIndex()))
		w.noteDrop("frame could not be mapped to v2: " + err.Error())
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

	if w.diskBytes >= maxDiskBufferBytes {
		return fmt.Errorf("disk buffer at its %d-byte cap", maxDiskBufferBytes)
	}

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
		w.diskBytes = 0
		w.drainConsumed = 0
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
	w.diskBytes += int64(len(data)) + 1
	if time.Since(w.diskSizeLogged) >= diskSizeLogInterval {
		w.diskSizeLogged = time.Now()
		w.logger.Info("Disk buffer size",
			zap.Int64("frames_buffered", w.diskFrameCount), zap.Int64("bytes", w.diskBytes),
			zap.Int64("cap_bytes", maxDiskBufferBytes))
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
		w.noteDropN(w.unreplayed(0), "disk buffer unreadable")
		w.cleanupDiskBuffer()
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially large JSON lines
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	sentCount := int64(0)
	batchCount := 0

	w.diskBufferMu.Lock()
	skip := w.drainConsumed
	w.diskBufferMu.Unlock()
	consumed := int64(0)

	for scanner.Scan() {
		if consumed < skip {
			consumed++ // sent by an earlier, aborted drain
			continue
		}
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
			w.keepAfterAbort(consumed)
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
			w.logger.Debug("Skipping unparseable buffered frame", zap.Error(err))
			w.noteDrop("unparseable buffered frame")
			consumed++
			continue
		}
		bf := buffered.GetFrame()
		if bf == nil {
			consumed++
			continue
		}
		if err := w.sendFrame(conn, bf); err != nil {
			w.logger.Warn("Failed to send buffered frame, will retry on next reconnect", zap.Error(err))
			w.keepAfterAbort(consumed)
			w.connLost(conn)
			return
		}
		// "Consumed" means written to the socket, not received: there are no acks,
		// so a frame that reaches the kernel buffer of a dying connection counts
		// as sent here and is lost if the server never reads it.
		consumed++
		if w.replayHook != nil {
			w.replayHook(consumed, conn)
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
		w.noteDropN(w.unreplayed(consumed), "disk buffer read error: "+err.Error())
	}

	w.logger.Info("Disk buffer drained successfully", zap.Int64("frames_sent", sentCount))
	w.cleanupDiskBuffer()
}

// unreplayed is how many spooled frames were not consumed by a replay.
func (w *WebSocketWriter) unreplayed(consumed int64) uint64 {
	w.diskBufferMu.Lock()
	defer w.diskBufferMu.Unlock()
	if n := w.diskFrameCount - consumed; n > 0 {
		return uint64(n)
	}
	return 0
}

// keepAfterAbort keeps the buffer after an aborted drain: it records how many
// lines were consumed, so the next drain resumes after them, and reopens the
// file for appending (the drain closed its write handle) so the spool can keep
// adding to it.
func (w *WebSocketWriter) keepAfterAbort(consumed int64) {
	w.diskBufferMu.Lock()
	defer w.diskBufferMu.Unlock()
	w.drainConsumed = consumed
	if !w.usingDiskBuffer || w.diskBufferPath == "" {
		return
	}
	f, err := os.OpenFile(w.diskBufferPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		w.logger.Warn("Failed to reopen disk buffer after aborted drain", zap.Error(err))
		return
	}
	w.diskBufferFile = f
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
	w.diskBytes = 0
	w.drainConsumed = 0
}
