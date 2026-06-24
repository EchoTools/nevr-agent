package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func makeFrame(t *testing.T, sessionID string, idx uint32, ts time.Time) *telemetry.LobbySessionStateFrame {
	t.Helper()
	return &telemetry.LobbySessionStateFrame{
		FrameIndex: idx,
		Timestamp:  timestamppb.New(ts),
		Session: &enginev1.SessionResponse{
			SessionId:  sessionID,
			GameStatus: "playing",
			MatchType:  "Echo_Arena",
			MapName:    "mpl_arena_a",
		},
	}
}

func TestTapeLogSession_WritesValidTapeFile(t *testing.T) {
	dir := filepath.Join("/var/tmp", "tape_test_"+t.Name())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	filePath := filepath.Join(dir, "test.tape")
	sessionID := "test-session-abc"
	logger := testLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := NewTapeLogSession(ctx, logger, filePath, sessionID)

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.ProcessFrames()
	}()

	baseTime := time.Date(2026, 6, 24, 15, 30, 45, 0, time.UTC)

	// Write 3 frames into the buffered channel.
	for i := uint32(0); i < 3; i++ {
		ts := baseTime.Add(time.Duration(i) * 100 * time.Millisecond)
		frame := makeFrame(t, sessionID, i, ts)
		if err := session.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
	}

	// Cancel the context. The priority-select design drains buffered frames
	// before checking ctx.Done(), so all 3 frames will be written.
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("ProcessFrames: %v", err)
	}

	// Read back and verify the tape file.
	reader, err := codec.NewReader(filePath)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer reader.Close()

	header, err := reader.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}

	if got := header.GetCaptureId(); got != sessionID {
		t.Errorf("header.CaptureId = %q, want %q", got, sessionID)
	}
	if got := header.GetFormatVersion(); got != 2 {
		t.Errorf("header.FormatVersion = %d, want 2", got)
	}

	eaHeader := header.GetEchoArena()
	if eaHeader == nil {
		t.Fatal("header.EchoArena is nil")
	}
	if got := eaHeader.GetSessionId(); got != sessionID {
		t.Errorf("eaHeader.SessionId = %q, want %q", got, sessionID)
	}
	if got := eaHeader.GetMapName(); got != "mpl_arena_a" {
		t.Errorf("eaHeader.MapName = %q, want %q", got, "mpl_arena_a")
	}

	// Read frames.
	var frameCount int
	for {
		_, err := reader.ReadFrame()
		if err != nil {
			break
		}
		frameCount++
	}
	if frameCount != 3 {
		t.Errorf("frame count = %d, want 3", frameCount)
	}

	// Read footer.
	footer, err := reader.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}
	if got := footer.GetFrameCount(); got != 3 {
		t.Errorf("footer.FrameCount = %d, want 3", got)
	}
}

func TestTapeLogSession_SessionIDChange(t *testing.T) {
	dir := filepath.Join("/var/tmp", "tape_test_"+t.Name())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	filePath := filepath.Join(dir, "test.tape")
	sessionID := "session-original"
	logger := testLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := NewTapeLogSession(ctx, logger, filePath, sessionID)
	session.outgoingCh = make(chan *telemetry.LobbySessionStateFrame)

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.ProcessFrames()
	}()

	baseTime := time.Date(2026, 6, 24, 15, 30, 45, 0, time.UTC)

	// Write first frame with matching session ID.
	frame1 := makeFrame(t, sessionID, 0, baseTime)
	session.outgoingCh <- frame1

	// Write second frame with a different session ID. ProcessFrames will
	// detect the mismatch and exit.
	frame2 := makeFrame(t, "session-different", 1, baseTime.Add(100*time.Millisecond))
	session.outgoingCh <- frame2

	// Wait for ProcessFrames to exit (it should stop due to session ID change).
	if err := <-errCh; err != nil {
		t.Fatalf("ProcessFrames: %v", err)
	}

	// Read back and verify only 1 frame was written.
	reader, err := codec.NewReader(filePath)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer reader.Close()

	if _, err := reader.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}

	var frameCount int
	for {
		_, err := reader.ReadFrame()
		if err != nil {
			break
		}
		frameCount++
	}
	if frameCount != 1 {
		t.Errorf("frame count = %d, want 1", frameCount)
	}

	footer, err := reader.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}
	if got := footer.GetFrameCount(); got != 1 {
		t.Errorf("footer.FrameCount = %d, want 1", got)
	}
}

func TestTapeSessionFilename(t *testing.T) {
	ts := time.Date(2026, 6, 24, 15, 30, 45, 0, time.UTC)
	got := TapeSessionFilename(ts, "abc-123")
	want := "rec_2026-06-24_15-30-45_abc-123.tape"
	if got != want {
		t.Errorf("TapeSessionFilename = %q, want %q", got, want)
	}
}
