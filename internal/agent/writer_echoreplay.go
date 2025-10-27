package agent

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/echotools/nevrcap"
	"go.uber.org/zap"
)

const (
	zipFileChunkSize = 2 * 1024 * 1024 // 64KB chunk size for zip file writing
)

var ErrSessionUUIDChanged = errors.New("session UUID changed")

type FrameDataLogSession struct {
	sync.Mutex
	ctx         context.Context
	ctxCancelFn context.CancelFunc
	logger      *zap.Logger

	filePath   string
	outgoingCh chan *rtapi.LobbySessionStateFrame
	buf        *bytes.Buffer

	sessionID string // Session ID for the current session
	stopped   bool
}

func (e *FrameDataLogSession) Context() context.Context {
	return e.ctx
}

func NewFrameDataLogSession(ctx context.Context, logger *zap.Logger, filePath string, sessionID string) *FrameDataLogSession {
	ctx, cancel := context.WithCancel(ctx)
	return &FrameDataLogSession{
		ctx:         ctx,
		ctxCancelFn: cancel,
		logger:      logger,

		filePath:   filePath,
		outgoingCh: make(chan *rtapi.LobbySessionStateFrame, 1000), // Buffered channel for outgoing frames
		buf:        bytes.NewBuffer(make([]byte, 0, 64*1024)),
		sessionID:  sessionID, // Initialize with the provided session ID
	}
}

func (fw *FrameDataLogSession) ProcessFrames() error {
	// Create a new zip file
	zf, err := os.Create(fw.filePath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}

	// Create a zip writer
	zw := zip.NewWriter(zf)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression) // Use best compression for the zip file
	})

	defer func() {
		logger := fw.logger.With(
			zap.String("file_path", fw.filePath),
			zap.String("session_id", fw.sessionID),
		)

		if err := zw.Flush(); err != nil {
			logger.Error("Failed to flush zip writer", zap.Error(err))
		}
		if err := zf.Sync(); err != nil {
			logger.Error("Failed to sync zip file", zap.Error(err))
		}

		if err := zw.Close(); err != nil {
			logger.Error("Failed to close zip writer", zap.Error(err))
		}
		if err := zf.Close(); err != nil {
			logger.Error("Failed to close zip file", zap.Error(err))
		}
	}()

	filename := filepath.Base(fw.filePath)

	// Create an identically named file inside the zip archive
	file, err := zw.Create(filename)
	if err != nil {
		return err
	}

	byteCount := 0

	writer, err := nevrcap.NewEchoReplayCodecWriter(fw.filePath)
	if err != nil {
		return fmt.Errorf("failed to create EchoReplayCodecWriter: %w", err)
	}

OuterLoop:
	for {
		select {
		case frame := <-fw.outgoingCh:
			// Write teh frame to the buffer
			fw.Lock()
			if fw.stopped { // If the writer is stopped, exit the loop
				fw.Unlock()
				break OuterLoop
			}

			// Extract the session UUID from the frame's session data
			sessionID := frame.GetSession().GetSessionId()
			if sessionID == "" {
				fw.logger.Error("Failed to extract session UUID from frame",
					zap.Any("data", frame.GetSession()),
					zap.Error(err))
				fw.Unlock()
				break OuterLoop
			}

			// If the session ID has changed, handle it
			if sessionID != fw.sessionID {
				fw.logger.Debug("Session UUID changed, stopping frame processing",
					zap.String("old_session_id", fw.sessionID),
					zap.String("new_session_id", sessionID),
				)
				fw.Unlock()
				break OuterLoop
			}

			// Write the frame to the buffer
			byteCount += writer.WriteReplayFrame(fw.buf, frame)
			// Check if the buffer has reached the chunk size
			if fw.buf.Len() >= zipFileChunkSize {
				// Write the buffer to the file
				if _, err := file.Write(fw.buf.Bytes()); err != nil {
					fw.logger.Error("Failed to write data to zip file",
						zap.String("file_path", fw.filePath),
						zap.Int("byte_count", byteCount),
						zap.Error(err),
					)
					fw.Unlock()
					break OuterLoop
				}
				fw.buf.Reset() // Clear the buffer after writing
			}
			fw.Unlock()

		case <-fw.ctx.Done():
			break OuterLoop
		}
	}

	fw.Close()

	fw.Lock()
	defer fw.Unlock()

	// Flush any remaining data in the buffer
	if fw.buf.Len() > 0 {
		if _, err := file.Write(fw.buf.Bytes()); err != nil {
			return fmt.Errorf("failed to write remaining data to zip file: %v", err)
		}
		fw.buf.Reset() // Clear the buffer after writing
	}

	fw.logger.Info("Echo replay file written",
		zap.String("file_path", fw.filePath),
		zap.Int("byte_count", byteCount),
	)
	return nil
}

func (fw *FrameDataLogSession) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if fw.IsStopped() {
		return fmt.Errorf("frame writer is stopped")
	}
	select {
	case fw.outgoingCh <- frame:
		return nil
	case <-fw.ctx.Done():
		return fmt.Errorf("context cancelled, cannot write frame: %w", fw.ctx.Err())
	default:
		return fmt.Errorf("outgoing channel is full, cannot write frame")
	}
}

func (fw *FrameDataLogSession) Close() {
	// Cancel any ongoing operations tied to this session.
	fw.ctxCancelFn()
	fw.Lock()
	if fw.stopped {
		fw.Unlock()
		return
	}
	fw.stopped = true
	fw.Unlock()
}

func (fw *FrameDataLogSession) IsStopped() bool {
	fw.Lock()
	defer fw.Unlock()
	return fw.stopped
}

func EchoReplaySessionFilename(ts time.Time, sessionID string) string {
	currentTime := ts.UTC().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("rec_%s_%s.echoreplay", currentTime, sessionID)
}
