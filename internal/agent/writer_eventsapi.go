package agent

import (
	"context"
	"fmt"
	"time"

	api "github.com/echotools/nevr-agent/v4/internal/api"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"go.uber.org/zap"
)

// EventsAPIWriter implements FrameWriter and posts frames to a session events API.
type EventsAPIWriter struct {
	logger      *zap.Logger
	client      *api.Client
	ctx         context.Context
	cancel      context.CancelFunc
	outgoingCh  chan *telemetry.LobbySessionStateFrame
	stopped     bool
	framesCount int64
	eventsSent  int64
	eventsURL   string
}

// NewEventsAPIWriter creates a new EventsAPIWriter with a background sender.
func NewEventsAPIWriter(logger *zap.Logger, baseURL, jwtToken string) *EventsAPIWriter {
	ctx, cancel := context.WithCancel(context.Background())

	c := api.NewClient(api.ClientConfig{
		BaseURL:  baseURL,
		Timeout:  5 * time.Second,
		JWTToken: jwtToken,
	})

	w := &EventsAPIWriter{
		logger:     logger.With(zap.String("component", "events_api_writer")),
		client:     c,
		ctx:        ctx,
		cancel:     cancel,
		outgoingCh: make(chan *telemetry.LobbySessionStateFrame, 1000),
		stopped:    false,
		eventsURL:  baseURL,
	}

	w.logger.Info("EventsAPIWriter initialized",
		zap.String("events_endpoint", baseURL))

	go w.run()
	return w
}

func (w *EventsAPIWriter) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case frame := <-w.outgoingCh:
			// Use a short timeout to avoid blocking the pipeline.
			ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
			resp, err := w.client.StoreSessionEvent(ctx, frame)
			if err != nil {
				w.logger.Warn("Failed to send session event",
					zap.Error(err),
					zap.String("url", w.eventsURL),
					zap.Int("event_count", len(frame.Events)))
			} else {
				w.eventsSent++
				w.logger.Debug("Session event sent successfully",
					zap.Int("event_count", len(frame.Events)),
					zap.Bool("success", resp.Success),
					zap.Int64("total_events_sent", w.eventsSent))
			}
			cancel()
		}
	}
}

// Context returns the writer context.
func (w *EventsAPIWriter) Context() context.Context { return w.ctx }

// WriteFrame enqueues a frame for sending to the events API.
func (w *EventsAPIWriter) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	if w.stopped {
		return fmt.Errorf("events api writer is stopped")
	}

	w.framesCount++

	// Skip frames without events
	if len(frame.Events) == 0 {
		if w.framesCount%1000 == 0 {
			w.logger.Debug("Skipping frames without events",
				zap.Int64("frames_processed", w.framesCount),
				zap.Int64("events_sent", w.eventsSent))
		}
		return nil
	}

	w.logger.Debug("Queueing frame with events",
		zap.Int("event_count", len(frame.Events)),
		zap.Int64("frame_index", int64(frame.FrameIndex)))

	select {
	case w.outgoingCh <- frame:
		return nil
	case <-w.ctx.Done():
		return fmt.Errorf("context cancelled: %w", w.ctx.Err())
	default:
		// Channel full; drop frame to preserve real-time behavior.
		w.logger.Warn("Dropping frame: outgoing channel full")
		return fmt.Errorf("outgoing channel full")
	}
}

// Close stops the writer.
func (w *EventsAPIWriter) Close() {
	if w.stopped {
		return
	}
	w.stopped = true
	w.cancel()
	w.logger.Info("Events API writer closed",
		zap.Int64("total_frames_processed", w.framesCount),
		zap.Int64("total_events_sent", w.eventsSent))
}

// IsStopped returns whether the writer is stopped.
func (w *EventsAPIWriter) IsStopped() bool { return w.stopped }
