package agent

import (
	"context"
	"fmt"

	rtapi "github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"go.uber.org/zap"
)

type FrameWriter interface {
	Context() context.Context
	WriteFrame(*rtapi.LobbySessionStateFrame) error
	Close()
	IsStopped() bool
}

type FrameReader interface {
	Context() context.Context
	ReadFrame() (*rtapi.LobbySessionStateFrame, error)
	Close()
}

// MultiWriter implements FrameWriter interface and writes to multiple FrameWriters
type MultiWriter struct {
	logger  *zap.Logger
	writers []FrameWriter
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool
}

// NewMultiWriter creates a new MultiWriter that writes to multiple FrameWriters
func NewMultiWriter(logger *zap.Logger, writers ...FrameWriter) *MultiWriter {
	ctx, cancel := context.WithCancel(context.Background())

	return &MultiWriter{
		logger:  logger.With(zap.String("component", "multi_writer"), zap.Int("writer_count", len(writers))),
		writers: writers,
		ctx:     ctx,
		cancel:  cancel,
		stopped: false,
	}
}

// Context returns the context for this writer
func (mw *MultiWriter) Context() context.Context {
	return mw.ctx
}

// WriteFrame writes frame data to all underlying writers
func (mw *MultiWriter) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if mw.stopped {
		return fmt.Errorf("multi writer is stopped")
	}

	var lastErr error
	successCount := 0

	for i, writer := range mw.writers {
		if writer.IsStopped() {
			mw.logger.Debug("Skipping stopped writer", zap.Int("writer_index", i))
			continue
		}

		if err := writer.WriteFrame(frame); err != nil {
			mw.logger.Error("Failed to write frame to writer", zap.Int("writer_index", i), zap.Error(err))
			lastErr = err
		} else {
			successCount++
		}
	}

	mw.logger.Debug("Wrote frame to writers",
		zap.Int("success_count", successCount),
		zap.Int("total_writers", len(mw.writers)))

	// Return error only if all writers failed
	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("all writers failed, last error: %w", lastErr)
	}

	return nil
}

// Close closes all underlying writers
func (mw *MultiWriter) Close() {
	if mw.stopped {
		return
	}

	mw.stopped = true
	mw.cancel()

	for i, writer := range mw.writers {
		writer.Close()
		mw.logger.Debug("Closed writer", zap.Int("writer_index", i))
	}

	mw.logger.Info("Multi writer closed")
}

// IsStopped returns whether the writer has been stopped
func (mw *MultiWriter) IsStopped() bool {
	return mw.stopped
}
