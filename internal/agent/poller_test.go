package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rtapi "github.com/echotools/nevr-common/gen/go/rtapi"
	"go.uber.org/zap"
)

func testLogger(t testing.TB) *zap.Logger {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return logger
}

type benchmarkWriter struct {
	frames []*rtapi.LobbySessionStateFrame
}

func (test *benchmarkWriter) Context() context.Context {
	return context.Background()
}
func (b *benchmarkWriter) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
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
		frames: make([]*rtapi.LobbySessionStateFrame, 0, b.N*2), // Preallocate space for frames
	}

	NewHTTPFramePoller(ctx, testLogger, http.DefaultClient, srv1.URL, interval, benchWriter)

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
