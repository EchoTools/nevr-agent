package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"go.uber.org/zap"
)

func TestPollIntervalForState(t *testing.T) {
	const active = time.Second / 30 // --frequency 30
	const idle = time.Second / 1    // --idle-fps 1 (default)

	cases := []struct {
		name       string
		gameStatus string
		cfg        PollerConfig
		want       time.Duration
	}{
		{
			// The bug: basic `--frequency 30` recording (no --active-only).
			// idle-fps defaults to 1, but the user did NOT opt into idle
			// bandwidth mode, so non-"playing" phases must stay at 30 Hz.
			name:       "basic recording keeps active rate during non-playing phase",
			gameStatus: "score",
			cfg:        PollerConfig{IdleFPS: 1, ActiveOnly: false},
			want:       active,
		},
		{
			name:       "basic recording keeps active rate while playing",
			gameStatus: "playing",
			cfg:        PollerConfig{IdleFPS: 1, ActiveOnly: false},
			want:       active,
		},
		{
			name:       "active-only mode drops to idle during non-playing phase",
			gameStatus: "social_lobby",
			cfg:        PollerConfig{IdleFPS: 1, ActiveOnly: true},
			want:       idle,
		},
		{
			name:       "active-only mode uses active rate while playing",
			gameStatus: "playing",
			cfg:        PollerConfig{IdleFPS: 1, ActiveOnly: true},
			want:       active,
		},
		{
			name:       "active-only with idle-fps disabled stays active",
			gameStatus: "social_lobby",
			cfg:        PollerConfig{IdleFPS: 0, ActiveOnly: true},
			want:       active,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pollIntervalForState(tc.gameStatus, tc.cfg, active, idle)
			if got != tc.want {
				t.Errorf("pollIntervalForState(%q, %+v) = %v, want %v", tc.gameStatus, tc.cfg, got, tc.want)
			}
		})
	}
}

// TestActiveGameplayRecordsAtFrequency encodes the reported bug in product
// terms: with `--frequency 30` and no --active-only, every phase of a real
// match must be polled at the active rate, so active gameplay records at ~30
// fps instead of collapsing to the default idle rate of 1 fps.
func TestActiveGameplayRecordsAtFrequency(t *testing.T) {
	const active = time.Second / 30
	const idle = time.Second
	cfg := PollerConfig{IdleFPS: 1, ActiveOnly: false}

	matchSequence := []string{
		"pre_match", "round_start", "playing", "playing",
		"score", "playing", "round_over", "post_match",
	}
	for _, status := range matchSequence {
		if got := pollIntervalForState(status, cfg, active, idle); got != active {
			t.Fatalf("status %q: interval %v, want active %v (basic recording must stay at --frequency)", status, got, active)
		}
	}
}

func testLogger(t testing.TB) *zap.Logger {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return logger
}

type benchmarkWriter struct {
	frames []*telemetry.LobbySessionStateFrame
}

func (test *benchmarkWriter) Context() context.Context {
	return context.Background()
}
func (b *benchmarkWriter) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	b.frames = append(b.frames, frame)
	return nil
}
func (b *benchmarkWriter) Close() {
	// Dummy close function that does nothing
}

func (b *benchmarkWriter) IsStopped() bool {
	return false // Always return false for the benchmark
}

func BenchmarkNewFrameLogger_TwoURLs_32KB_MaxPollingRate(b *testing.B) {
	testLogger := testLogger(b)

	const (
		frameLength      = 64 * 1024 // 64KB payload size
		pollingFrequency = 10000     // 10,000Hz
		interval         = time.Second / pollingFrequency
	)

	payload := make([]byte, frameLength)
	for i := range payload {
		payload[i] = byte(i % 256) // Fill with some data
	}

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer time.AfterFunc(100*time.Millisecond, func() { srv1.Close() })

	<-time.After(100 * time.Millisecond) // Ensure servers are ready

	b.Logf("Benchmarking with %d URLs, each returning %d bytes of data at %d FPS", 2, frameLength, pollingFrequency)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	benchWriter := &benchmarkWriter{
		frames: make([]*telemetry.LobbySessionStateFrame, 0, b.N*2), // Preallocate space for frames
	}

	NewHTTPFramePoller(ctx, testLogger, http.DefaultClient, srv1.URL, interval, benchWriter, PollerConfig{})

	b.Logf("Warmed up channel, ready for benchmark with %d frames", b.N)

	for i := 0; b.Loop(); i++ {
		select {
		case <-ctx.Done():
			b.Fatal("context cancelled before benchmark completed")
		default:
			// Simulate receiving frames
			if len(benchWriter.frames) < i {
				b.Fatalf("expected at least %d bytes of data, got %d", frameLength*2, len(benchWriter.frames))
			}
		}
	}

	b.ReportAllocs()
	b.StopTimer()
}
