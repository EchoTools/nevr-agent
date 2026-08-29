package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// closeRecorder is a FrameWriter that records when Close() runs. It is the
// discriminator for the behaviour proven live 2026-08-28: when the game's
// /session stops responding at match end, the poller's 5s no-data timeout
// returns the goroutine and its `defer session.Close()` fires -- which for the
// WebSocket writer is what sends the CaptureFooter that ends the match cleanly
// on nevr-stream. Without that, a real match never sends a footer and every
// stream sits on nevr-stream's 60s truncation reap instead.
type closeRecorder struct {
	ctx      context.Context
	mu       sync.Mutex
	closedAt time.Time
}

func (c *closeRecorder) Context() context.Context                           { return c.ctx }
func (c *closeRecorder) WriteFrame(*telemetry.LobbySessionStateFrame) error { return nil }
func (c *closeRecorder) IsStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closedAt.IsZero()
}
func (c *closeRecorder) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closedAt.IsZero() {
		c.closedAt = time.Now()
	}
}
func (c *closeRecorder) when() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closedAt
}

// TestPollerClosesSessionWhenSourceStops drives the real poller against a source
// that serves data and then goes away, and asserts the session is closed shortly
// after -- the trigger for the match-end footer, witnessed here as a Close()
// rather than a footer so the test does not depend on /session JSON parsing.
func TestPollerClosesSessionWhenSourceStops(t *testing.T) {
	var live struct {
		sync.Mutex
		up bool
	}
	live.up = true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		live.Lock()
		up := live.up
		live.Unlock()
		if !up {
			// The game server has ended the match / gone away.
			http.Error(w, "no session", http.StatusNotFound)
			return
		}
		// A non-empty 200 body keeps the poller's no-data timeout from firing.
		// It need not parse into a frame: the timeout resets on a non-empty
		// response, which is exactly the liveness signal at issue.
		_, _ = w.Write([]byte(`{"session_id":"s","game_status":"playing"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rec := &closeRecorder{ctx: ctx}

	go NewHTTPFramePoller(ctx, testLogger(t), http.DefaultClient, srv.URL, 20*time.Millisecond, rec, PollerConfig{})

	// While the source is up, the session must stay open: a poller that closes
	// during a live match would truncate every capture.
	time.Sleep(2 * time.Second)
	if rec.IsStopped() {
		t.Fatal("session closed while the source was still serving data")
	}

	// The match ends: the source stops responding.
	stopAt := time.Now()
	live.Lock()
	live.up = false
	live.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for rec.when().IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("session was NOT closed within 10s of the source going away -- " +
				"the match-end footer would never be sent (poller timeout or defer session.Close() broken)")
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := rec.when().Sub(stopAt)
	t.Logf("session closed %v after the source stopped (the 5s no-data timeout)", elapsed.Round(100*time.Millisecond))
	if elapsed > 9*time.Second {
		t.Errorf("session took %v to close; the no-data timeout should fire in ~5s", elapsed)
	}
}
