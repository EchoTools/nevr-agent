package agent

import (
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketWriter_OneWriteFailureCausesOneReconnect reproduces the
// self-sustaining reconnect loop seen against nevr-stream (a producer connecting
// and being closed about once per second all night).
//
// One write fails. reconnectLoop closes the old conn while the old readLoop is
// still blocked on it; that readLoop then errors, and -- before the fix -- marks
// the NEW connection disconnected and triggers another reconnect, which closes
// the new connection, whose readLoop does the same, forever. Each cycle also
// leaked a writeLoop.
//
// The server never closes a connection, so every close the client sees is one the
// client caused.
func TestWebSocketWriter_OneWriteFailureCausesOneReconnect(t *testing.T) {
	var accepted atomic.Int64
	var mu sync.Mutex
	var perConn []int // messages received, by connection index
	release := make(chan struct{})
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mu.Lock()
		idx := len(perConn)
		perConn = append(perConn, 0)
		mu.Unlock()
		accepted.Add(1)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				// Do not hang up: a half-closed client must keep its read
				// side blocked, as in the field.
				<-release
				return
			}
			mu.Lock()
			perConn[idx]++
			mu.Unlock()
		}
	}))
	defer srv.Close()
	defer close(release)

	w := NewWebSocketWriter(testLogger(t), "ws"+strings.TrimPrefix(srv.URL, "http"), "")
	if err := w.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer w.Close()

	// Force exactly one write failure: shut the write half of the first conn.
	w.mu.Lock()
	tcp, ok := w.conn.UnderlyingConn().(*net.TCPConn)
	w.mu.Unlock()
	if !ok {
		t.Fatal("underlying conn is not a TCPConn")
	}
	if err := tcp.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	_ = w.WriteFrame(makeRosterFrame("sess-loop", 0, base))

	// Let the reconnect (1s backoff) land and the system settle.
	time.Sleep(3 * time.Second)
	settled := accepted.Load()
	g1 := runtime.NumGoroutine()

	// A real loop reconnects about once per second; watch several cycles.
	time.Sleep(5 * time.Second)
	g2 := runtime.NumGoroutine()

	if got := accepted.Load(); got != settled || got != 2 {
		t.Errorf("connections accepted: %d at settle, %d after 5s more; want exactly 2 (original + one reconnect) and no further reconnects", settled, got)
	}
	if g2 > g1 {
		t.Errorf("goroutines grew while idle: %d -> %d (a leaked loop per reconnect cycle)", g1, g2)
	}

	w.mu.Lock()
	connected := w.connected
	w.mu.Unlock()
	if !connected {
		t.Error("writer is left disconnected; no frames would be sent")
	}

	// The surviving connection must actually carry frames.
	_ = w.WriteFrame(makeRosterFrame("sess-loop", 1, base.Add(100*time.Millisecond)))
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(perConn) < 2 || perConn[len(perConn)-1] == 0 {
		t.Errorf("newest connection received no messages: %v", perConn)
	}
}
