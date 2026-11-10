package agent

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/protobuf/encoding/protojson"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// TestWebSocketWriter_AbortedReplayResumesWithoutDuplicates: the connection dies
// mid-replay. nevr-stream opens a new piece per connection under the same
// session id, so the next replay must send exactly k+1..N: the union the ingest
// received has no duplicate frame index and no gap, and each piece's footer
// counts its own frames.
func TestWebSocketWriter_AbortedReplayResumesWithoutDuplicates(t *testing.T) {
	var up bool
	var mu sync.Mutex
	var pieces [][]received // one slice of messages per accepted connection
	upg := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := up
		mu.Unlock()
		if !ok {
			http.Error(rw, "down", http.StatusServiceUnavailable)
			return
		}
		conn, err := upg.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mu.Lock()
		idx := len(pieces)
		pieces = append(pieces, nil)
		mu.Unlock()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			pieces[idx] = append(pieces[idx], received{messageType: mt, data: data})
			mu.Unlock()
		}
	}))
	defer srv.Close()

	w := NewWebSocketWriter(testLogger(t), "ws"+srv.URL[len("http"):], "")
	const frames, killAfter = 1200, 400
	var killed sync.Once
	w.replayHook = func(consumed int64, conn *websocket.Conn) {
		if consumed == killAfter {
			killed.Do(func() { conn.Close() }) // the next write fails: replay aborts after exactly k frames
		}
	}
	if err := w.ConnectWithRetry(); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < frames; i++ {
		if err := w.WriteFrame(makeRosterFrame("sess-replay", uint32(i), base.Add(time.Duration(i)*10*time.Millisecond))); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	up = true
	mu.Unlock()

	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, p := range pieces {
			for _, m := range p {
				if parseIngestEnvelope(t, m.data).GetFrame() != nil {
					n++
				}
			}
		}
		return n
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && count() < frames {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	w.Close() // footers the live piece
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(pieces) < 2 {
		t.Fatalf("got %d connections, want the aborted one plus a resume", len(pieces))
	}
	next := uint32(0)
	lastFooter := false
	for pi, p := range pieces {
		lastFooter = false
		if parseIngestEnvelope(t, p[0].data).GetHeader() == nil {
			t.Fatalf("piece %d does not start with a header", pi)
		}
		n := uint32(0)
		for _, m := range p[1:] {
			env := parseIngestEnvelope(t, m.data)
			if f := env.GetFrame(); f != nil {
				if f.GetFrameIndex() != next {
					t.Fatalf("piece %d: frame_index %d, want %d (duplicate or gap)", pi, f.GetFrameIndex(), next)
				}
				next++
				n++
			}
			if ft := env.GetFooter(); ft != nil {
				lastFooter = true
				if ft.GetFrameCount() != n {
					t.Errorf("piece %d footer counts %d frames, piece carried %d", pi, ft.GetFrameCount(), n)
				}
			}
		}
	}
	if !lastFooter {
		t.Error("the last piece has no footer")
	}
	if next != frames {
		t.Errorf("ingest received frames up to %d, want %d", next, frames)
	}
}

func TestWebSocketWriter_ReplayCountsUnparseableLineAsDrop(t *testing.T) {
	srv := newWireServer(t, 0)
	w := NewWebSocketWriter(testLogger(t), srv.wsURL(), "")
	if err := w.Connect(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: true}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := w.bufferToDisk(makeRosterFrame("sess-bad", uint32(i), base), &m); err != nil {
			t.Fatal(err)
		}
	}
	w.diskBufferMu.Lock()
	_, _ = w.diskBufferFile.WriteString("{not json\n")
	w.diskBufferMu.Unlock()

	w.drainDiskBuffer()

	w.dropMu.Lock()
	dropped := w.dropped
	w.dropMu.Unlock()
	if dropped != 1 {
		t.Errorf("dropped_total = %d, want 1 (the unparseable line)", dropped)
	}
}

// A drain that finds no connection must keep the spool usable: the write handle
// it closed has to be reopened, or every later spooled frame fails.
func TestWebSocketWriter_DrainWithoutConnKeepsSpoolWritable(t *testing.T) {
	w := NewWebSocketWriter(testLogger(t), "ws://127.0.0.1:1", "")
	defer w.Close()
	m := protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: true}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := w.bufferToDisk(makeRosterFrame("sess-keep", 0, base), &m); err != nil {
		t.Fatal(err)
	}
	w.drainDiskBuffer() // not connected: aborts
	if err := w.bufferToDisk(makeRosterFrame("sess-keep", 1, base), &m); err != nil {
		t.Errorf("spool unusable after an aborted drain: %v", err)
	}
	if _, err := os.Stat(w.diskBufferPath); err != nil {
		t.Errorf("spool file gone: %v", err)
	}
}

// The spool is capped: past the cap frames are dropped, counted and logged, and
// the file stops growing.
func TestWebSocketWriter_SpoolCapDropsAndCounts(t *testing.T) {
	old := maxDiskBufferBytes
	maxDiskBufferBytes = 20 << 10
	defer func() { maxDiskBufferBytes = old }()

	core, logs := observer.New(zapcore.DebugLevel)
	w := NewWebSocketWriter(zap.New(core), "ws://127.0.0.1:1", "") // refused, retries with backoff
	if err := w.ConnectWithRetry(); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		_ = w.WriteFrame(makeRosterFrame("sess-cap", uint32(i), base))
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	w.diskBufferMu.Lock()
	size := w.diskBytes
	w.diskBufferMu.Unlock()
	if size > maxDiskBufferBytes+4096 {
		t.Errorf("spool is %d bytes, cap %d", size, maxDiskBufferBytes)
	}
	w.dropMu.Lock()
	dropped := w.dropped
	w.dropMu.Unlock()
	if dropped == 0 {
		t.Error("no drops counted at the cap")
	}
	found := false
	for _, e := range logs.All() {
		if _, ok := e.ContextMap()["dropped_total"]; ok {
			found = true
		}
	}
	if !found {
		t.Error("the cap was hit silently: no drop line logged")
	}
	w.Close()
}

var _ = telemetry.LobbySessionStateFrame{}

// TestWebSocketWriter_FrameInFlightWhenConnDiesIsNotLost: a live write fails on
// a connected conn. The frame in hand must be spooled ahead of anything newer
// and re-sent after the reconnect: the union has no duplicate and no gap.
func TestWebSocketWriter_FrameInFlightWhenConnDiesIsNotLost(t *testing.T) {
	var mu sync.Mutex
	var pieces [][]received
	upg := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := upg.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mu.Lock()
		idx := len(pieces)
		pieces = append(pieces, nil)
		mu.Unlock()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				<-release // never hang up: the client's read side stays blocked
				return
			}
			mu.Lock()
			pieces[idx] = append(pieces[idx], received{messageType: mt, data: data})
			mu.Unlock()
		}
	}))
	defer srv.Close()
	defer close(release)

	w := NewWebSocketWriter(testLogger(t), "ws"+strings.TrimPrefix(srv.URL, "http"), "")
	if err := w.Connect(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	send := func(i int) {
		_ = w.WriteFrame(makeRosterFrame("sess-live", uint32(i), base.Add(time.Duration(i)*10*time.Millisecond)))
		time.Sleep(20 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		send(i)
	}
	// Kill the write half only: the next live write fails while the conn still
	// looks connected, so that frame is the one in hand.
	w.mu.Lock()
	tcp := w.conn.UnderlyingConn().(*net.TCPConn)
	w.mu.Unlock()
	if err := tcp.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	for i := 10; i < 20; i++ {
		send(i)
	}
	time.Sleep(4 * time.Second) // reconnect backoff + replay

	mu.Lock()
	defer mu.Unlock()
	next := uint32(0)
	for pi, p := range pieces {
		for _, m := range p {
			if f := parseIngestEnvelope(t, m.data).GetFrame(); f != nil {
				if f.GetFrameIndex() != next {
					t.Fatalf("piece %d: frame_index %d, want %d (the in-flight frame was lost or duplicated)", pi, f.GetFrameIndex(), next)
				}
				next++
			}
		}
	}
	if next != 20 {
		t.Errorf("ingest received frames up to %d, want 20", next)
	}
}

// A spool line over the scanner limit aborts the drain; the frames behind it
// are lost, and must be counted, not deleted silently.
func TestWebSocketWriter_ScannerErrorCountsTheLostRemainder(t *testing.T) {
	srv := newWireServer(t, 0)
	w := NewWebSocketWriter(testLogger(t), srv.wsURL(), "")
	if err := w.Connect(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: true}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := w.bufferToDisk(makeRosterFrame("sess-big", 0, base), &m); err != nil {
		t.Fatal(err)
	}
	w.diskBufferMu.Lock()
	_, _ = w.diskBufferFile.WriteString(strings.Repeat("x", 11<<20) + "\n") // over the 10 MiB limit
	w.diskBufferMu.Unlock()
	for i := 1; i <= 2; i++ {
		if err := w.bufferToDisk(makeRosterFrame("sess-big", uint32(i), base), &m); err != nil {
			t.Fatal(err)
		}
	}

	w.drainDiskBuffer()

	w.dropMu.Lock()
	dropped := w.dropped
	w.dropMu.Unlock()
	if dropped != 2 {
		t.Errorf("dropped_total = %d, want 2 (the frames behind the oversized line)", dropped)
	}
}
