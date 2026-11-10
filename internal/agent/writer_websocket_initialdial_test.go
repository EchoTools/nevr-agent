package agent

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketWriter_InitialDialRefusedThenAccepted: the ingest refuses the
// first N dials at match start, then accepts. The session's frames -- written
// while the connection is still down -- must arrive, header first. Before this
// change the first failed dial dropped the writer for the whole session.
func TestWebSocketWriter_InitialDialRefusedThenAccepted(t *testing.T) {
	const refuse = 2
	var dials atomic.Int64
	var mu sync.Mutex
	var msgs []received
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if dials.Add(1) <= refuse {
			http.Error(rw, "ingest not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := up.Upgrade(rw, r, nil)
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
		t.Fatalf("ConnectWithRetry returned %v; a refused dial must be retried, not returned", err)
	}
	defer w.Close()

	const frames = 3
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < frames; i++ {
		if err := w.WriteFrame(makeRosterFrame("sess-dial", uint32(i), base.Add(time.Duration(i)*100*time.Millisecond))); err != nil {
			t.Fatalf("WriteFrame(%d) while the ingest is down: %v", i, err)
		}
	}

	// Backoff: immediate dial fails, +1s fails, +2s succeeds.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= frames+1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(msgs) < frames+1 {
		t.Fatalf("ingest received %d messages after %d refused dials, want header + %d frames", len(msgs), refuse, frames)
	}
	if parseIngestEnvelope(t, msgs[0].data).GetHeader() == nil {
		t.Fatal("first message is not a header")
	}
	for i := 1; i <= frames; i++ {
		f := parseIngestEnvelope(t, msgs[i].data).GetFrame()
		if f == nil || f.GetFrameIndex() != uint32(i-1) {
			t.Fatalf("message %d: want frame_index %d, got %v", i, i-1, f)
		}
	}
}
