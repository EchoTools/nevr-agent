package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/conversion"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapeLogSession writes frames to a .tape v2 format file using tape's codec.Writer
// and conversion.FrameMapper. The header is deferred until the first frame arrives,
// because MapHeaderFromSession needs the session data.
type TapeLogSession struct {
	sync.Mutex
	ctx         context.Context
	ctxCancelFn context.CancelFunc
	logger      *zap.Logger

	filePath   string
	outgoingCh chan *telemetry.LobbySessionStateFrame

	sessionID string
	stopped   bool
}

func (t *TapeLogSession) Context() context.Context {
	return t.ctx
}

// NewTapeLogSession creates a new tape v2 file writer session.
func NewTapeLogSession(ctx context.Context, logger *zap.Logger, filePath string, sessionID string) *TapeLogSession {
	ctx, cancel := context.WithCancel(ctx)
	return &TapeLogSession{
		ctx:         ctx,
		ctxCancelFn: cancel,
		logger:      logger,

		filePath:   filePath,
		outgoingCh: make(chan *telemetry.LobbySessionStateFrame, 1000),
		sessionID:  sessionID,
	}
}

// ProcessFrames runs the frame processing loop. It waits for the first frame to
// build the capture header, then maps and writes subsequent frames until the
// context is cancelled or the session ID changes.
func (t *TapeLogSession) ProcessFrames() error {
	// Wait for the first frame to build the header. Priority select ensures
	// a pending frame is consumed before checking context cancellation.
	var firstFrame *telemetry.LobbySessionStateFrame
	select {
	case firstFrame = <-t.outgoingCh:
	default:
		select {
		case firstFrame = <-t.outgoingCh:
		case <-t.ctx.Done():
			t.Close()
			return nil
		}
	}

	t.Lock()
	if t.stopped {
		t.Unlock()
		t.Close()
		return nil
	}

	sessionID := firstFrame.GetSession().GetSessionId()
	if sessionID == "" {
		t.logger.Error("failed to extract session ID from first frame",
			zap.Any("session", firstFrame.GetSession()))
		t.Unlock()
		t.Close()
		return nil
	}

	if sessionID != t.sessionID {
		t.logger.Debug("session ID mismatch on first frame",
			zap.String("expected", t.sessionID),
			zap.String("got", sessionID),
		)
		t.Unlock()
		t.Close()
		return nil
	}
	t.Unlock()

	// Create the tape writer after validating the first frame.
	writer, err := codec.NewWriter(t.filePath)
	if err != nil {
		t.Close()
		return fmt.Errorf("writer_tape: failed to create tape writer: %w", err)
	}

	defer func() {
		if err := writer.Close(); err != nil {
			t.logger.Error("failed to close tape writer", zap.Error(err))
		}
	}()

	// Build header from the first frame's session data.
	v1hdr := &telemetry.TelemetryHeader{
		CaptureId: t.sessionID,
		CreatedAt: timestamppb.New(firstFrame.GetTimestamp().AsTime()),
		Metadata: map[string]string{
			"format": "tape",
		},
	}
	captureHeader := conversion.MapHeaderFromSession(v1hdr, firstFrame.GetSession())
	if err := writer.WriteHeader(captureHeader); err != nil {
		return fmt.Errorf("writer_tape: failed to write header: %w", err)
	}

	// Initialize the frame mapper with the first frame's timestamp as base time.
	mapper := conversion.FrameMapper{
		BaseTime: firstFrame.GetTimestamp().AsTime(),
	}

	// Map and write the first frame.
	v2frame := mapper.MapFrame(firstFrame)
	if err := writer.WriteFrame(v2frame); err != nil {
		return fmt.Errorf("writer_tape: failed to write first frame: %w", err)
	}
	frameCount := 1

	// Process remaining frames. The priority select drains pending frames
	// before checking the context, so frames already in the channel at
	// shutdown are not silently dropped.
OuterLoop:
	for {
		// Priority: drain any pending frame before checking context.
		var frame *telemetry.LobbySessionStateFrame
		select {
		case frame = <-t.outgoingCh:
		default:
			// Channel empty — block on both.
			select {
			case frame = <-t.outgoingCh:
			case <-t.ctx.Done():
				break OuterLoop
			}
		}

		t.Lock()
		if t.stopped {
			t.Unlock()
			break OuterLoop
		}
		sessionID := t.sessionID
		t.Unlock()

		gotID := frame.GetSession().GetSessionId()
		if gotID == "" {
			t.logger.Error("frame has no session ID",
				zap.Any("session", frame.GetSession()))
			break OuterLoop
		}

		if gotID != sessionID {
			t.logger.Debug("session ID changed, stopping frame processing",
				zap.String("old_session_id", sessionID),
				zap.String("new_session_id", gotID))
			break OuterLoop
		}

		v2frame := mapper.MapFrame(frame)
		if err := writer.WriteFrame(v2frame); err != nil {
			t.logger.Error("failed to write frame to tape file",
				zap.String("file_path", t.filePath),
				zap.Error(err))
			break OuterLoop
		}
		frameCount++
	}

	t.Close()

	t.logger.Info("Tape file written",
		zap.String("file_path", t.filePath),
		zap.Int("frame_count", frameCount),
	)
	return nil
}

func (t *TapeLogSession) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	if t.IsStopped() {
		return fmt.Errorf("writer_tape: frame writer is stopped")
	}
	select {
	case t.outgoingCh <- frame:
		return nil
	case <-t.ctx.Done():
		return fmt.Errorf("writer_tape: context cancelled, cannot write frame: %w", t.ctx.Err())
	default:
		return fmt.Errorf("writer_tape: outgoing channel is full, cannot write frame")
	}
}

func (t *TapeLogSession) Close() {
	t.ctxCancelFn()
	t.Lock()
	if t.stopped {
		t.Unlock()
		return
	}
	t.stopped = true
	t.Unlock()
}

func (t *TapeLogSession) IsStopped() bool {
	t.Lock()
	defer t.Unlock()
	return t.stopped
}

// TapeSessionFilename generates a filename for a tape session.
func TapeSessionFilename(ts time.Time, sessionID string) string {
	currentTime := ts.UTC().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("rec_%s_%s.tape", currentTime, sessionID)
}
