package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NevrCapLogSession writes frames to a .nevrcap file (zstd compressed protobuf)
type NevrCapLogSession struct {
	sync.Mutex
	ctx         context.Context
	ctxCancelFn context.CancelFunc
	logger      *zap.Logger

	filePath   string
	outgoingCh chan *telemetry.LobbySessionStateFrame

	sessionID string
	stopped   bool
}

func (n *NevrCapLogSession) Context() context.Context {
	return n.ctx
}

// NewNevrCapLogSession creates a new nevrcap file writer session
func NewNevrCapLogSession(ctx context.Context, logger *zap.Logger, filePath string, sessionID string) *NevrCapLogSession {
	ctx, cancel := context.WithCancel(ctx)
	return &NevrCapLogSession{
		ctx:         ctx,
		ctxCancelFn: cancel,
		logger:      logger,

		filePath:   filePath,
		outgoingCh: make(chan *telemetry.LobbySessionStateFrame, 1000),
		sessionID:  sessionID,
	}
}

func (n *NevrCapLogSession) ProcessFrames() error {
	// Create a new nevrcap writer
	writer, err := codecs.NewNevrCapWriter(n.filePath)
	if err != nil {
		return fmt.Errorf("failed to create nevrcap writer: %w", err)
	}

	defer func() {
		if err := writer.Close(); err != nil {
			n.logger.Error("Failed to close nevrcap writer", zap.Error(err))
		}
	}()

	// Write header
	header := &telemetry.TelemetryHeader{
		CaptureId: n.sessionID,
		CreatedAt: timestamppb.Now(),
		Metadata: map[string]string{
			"format": "nevrcap",
		},
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	frameCount := 0

OuterLoop:
	for {
		select {
		case frame := <-n.outgoingCh:
			n.Lock()
			if n.stopped {
				n.Unlock()
				break OuterLoop
			}

			// Extract the session UUID from the frame's session data
			sessionID := frame.GetSession().GetSessionId()
			if sessionID == "" {
				n.logger.Error("Failed to extract session UUID from frame",
					zap.Any("data", frame.GetSession()))
				n.Unlock()
				break OuterLoop
			}

			// If the session ID has changed, handle it
			if sessionID != n.sessionID {
				n.logger.Debug("Session UUID changed, stopping frame processing",
					zap.String("old_session_id", n.sessionID),
					zap.String("new_session_id", sessionID),
				)
				n.Unlock()
				break OuterLoop
			}

			// Write the frame
			if err := writer.WriteFrame(frame); err != nil {
				n.logger.Error("Failed to write frame to nevrcap file",
					zap.String("file_path", n.filePath),
					zap.Error(err),
				)
				n.Unlock()
				break OuterLoop
			}
			frameCount++
			n.Unlock()

		case <-n.ctx.Done():
			break OuterLoop
		}
	}

	n.Close()

	n.logger.Info("NevrCap file written",
		zap.String("file_path", n.filePath),
		zap.Int("frame_count", frameCount),
	)
	return nil
}

func (n *NevrCapLogSession) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	if n.IsStopped() {
		return fmt.Errorf("frame writer is stopped")
	}
	select {
	case n.outgoingCh <- frame:
		return nil
	case <-n.ctx.Done():
		return fmt.Errorf("context cancelled, cannot write frame: %w", n.ctx.Err())
	default:
		return fmt.Errorf("outgoing channel is full, cannot write frame")
	}
}

func (n *NevrCapLogSession) Close() {
	n.ctxCancelFn()
	n.Lock()
	if n.stopped {
		n.Unlock()
		return
	}
	n.stopped = true
	n.Unlock()
}

func (n *NevrCapLogSession) IsStopped() bool {
	n.Lock()
	defer n.Unlock()
	return n.stopped
}

// NevrCapSessionFilename generates a filename for a nevrcap session
func NevrCapSessionFilename(ts time.Time, sessionID string) string {
	currentTime := ts.UTC().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("rec_%s_%s.nevrcap", currentTime, sessionID)
}
