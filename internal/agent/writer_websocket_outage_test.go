package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type recordingWriter struct {
	mu     sync.Mutex
	frames []*telemetry.LobbySessionStateFrame
	ctx    context.Context
}

func (r *recordingWriter) Context() context.Context { return r.ctx }
func (r *recordingWriter) Close()                   {}
func (r *recordingWriter) IsStopped() bool          { return false }
func (r *recordingWriter) WriteFrame(f *telemetry.LobbySessionStateFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, f)
	return nil
}

// TestWebSocketWriter_BlackholedIngestDoesNotStallWriteFrame: the ingest accepts
// TCP and never answers the handshake (45 s default timeout). WriteFrame must
// not block on the dial, and a file writer beside it must get every frame.
func TestWebSocketWriter_BlackholedIngestDoesNotStallWriteFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var held []net.Conn
	var heldMu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			heldMu.Lock()
			held = append(held, c)
			heldMu.Unlock()
		}
	}()
	defer func() {
		heldMu.Lock()
		for _, c := range held {
			c.Close()
		}
		heldMu.Unlock()
	}()

	w := NewWebSocketWriter(testLogger(t), "ws://"+ln.Addr().String(), "")
	file := &recordingWriter{ctx: context.Background()}
	mw := NewMultiWriter(testLogger(t), file, w)

	start := time.Now()
	if err := w.ConnectWithRetry(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("ConnectWithRetry blocked %v on a blackholed ingest", d)
	}
	time.Sleep(300 * time.Millisecond) // let the dial be in flight

	const frames = 20
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < frames; i++ {
		t0 := time.Now()
		_ = mw.WriteFrame(makeRosterFrame("sess-hole", uint32(i), base.Add(time.Duration(i)*100*time.Millisecond)))
		if d := time.Since(t0); d > 500*time.Millisecond {
			t.Fatalf("WriteFrame(%d) blocked %v behind the dial", i, d)
		}
	}
	file.mu.Lock()
	got := len(file.frames)
	file.mu.Unlock()
	if got != frames {
		t.Errorf("file writer got %d frames, want %d", got, frames)
	}
	done := make(chan struct{})
	go func() { w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked behind the in-flight dial")
	}
}

// TestWebSocketWriter_LongOutageBeforeFirstConnectLosesNothing: an outage longer
// than the 1000-frame memory buffer at match start; once the ingest comes up,
// every frame arrives, in order, behind one header.
func TestWebSocketWriter_LongOutageBeforeFirstConnectLosesNothing(t *testing.T) {
	var up atomic.Bool
	var mu sync.Mutex
	var msgs []received
	upg := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(rw, "down", http.StatusServiceUnavailable)
			return
		}
		conn, err := upg.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			msgs = append(msgs, received{messageType: mt, data: data})
			mu.Unlock()
		}
	}))
	defer srv.Close()

	w := NewWebSocketWriter(testLogger(t), "ws"+srv.URL[len("http"):], "")
	if err := w.ConnectWithRetry(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const frames = 1500
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < frames; i++ {
		if err := w.WriteFrame(makeRosterFrame("sess-outage", uint32(i), base.Add(time.Duration(i)*10*time.Millisecond))); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
		time.Sleep(time.Millisecond)
	}
	up.Store(true)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= frames+1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != frames+1 {
		t.Fatalf("ingest got %d messages, want header + %d frames", len(msgs), frames)
	}
	if parseIngestEnvelope(t, msgs[0].data).GetHeader() == nil {
		t.Fatal("first message is not a header")
	}
	for i := 0; i < frames; i++ {
		f := parseIngestEnvelope(t, msgs[i+1].data).GetFrame()
		if f == nil || f.GetFrameIndex() != uint32(i) {
			t.Fatalf("message %d: want frame_index %d, got %v", i+1, i, f)
		}
	}
}

// TestWebSocketWriter_DropsAreLoggedOnceWithCount: when frames can still be
// dropped (no loop draining, channel full) the first drop is logged with a
// running count and the session-scoped logger's fields, not one line per frame.
func TestWebSocketWriter_DropsAreLoggedOnceWithCount(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	w := NewWebSocketWriter(zap.New(core).With(zap.String("session_uuid", "sess-drop")), "ws://127.0.0.1:1", "")
	defer w.Close()

	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < memoryBufferSize+500; i++ {
		_ = w.WriteFrame(makeRosterFrame("sess-drop", uint32(i), base))
	}
	var drops []observer.LoggedEntry
	for _, e := range logs.All() {
		if _, ok := e.ContextMap()["dropped_total"]; ok {
			drops = append(drops, e)
		}
	}
	if len(drops) != 1 {
		t.Fatalf("got %d drop log lines for 500 drops, want 1", len(drops))
	}
	if drops[0].ContextMap()["session_uuid"] != "sess-drop" {
		t.Errorf("drop line lacks the session id: %v", drops[0].ContextMap())
	}
}

// TestWebSocketWriter_CloseWakesBackoffSleep: Close must end the supervisor
// promptly, not after the rest of a (up to 30 s) backoff sleep.
func TestWebSocketWriter_CloseWakesBackoffSleep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Error(rw, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	w := NewWebSocketWriter(testLogger(t), "ws"+srv.URL[len("http"):], "")
	if err := w.ConnectWithRetry(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // first dial failed, a backoff sleep is under way
	w.Close()
	select {
	case <-w.supervisorDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reconnectLoop still sleeping 500ms after Close")
	}
}
