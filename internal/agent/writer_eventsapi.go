package agent

import (
	"context"
	"fmt"
	"time"

	api "github.com/echotools/evr-data-recorder/v3/internal/api"
	rtapi "github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"go.uber.org/zap"
)

// EventsAPIWriter implements FrameWriter and posts frames to a /lobby-session-events HTTP API.
type EventsAPIWriter struct {
	logger     *zap.Logger
	client     *api.Client
	ctx        context.Context
	cancel     context.CancelFunc
	outgoingCh chan *rtapi.LobbySessionStateFrame
	stopped    bool
}

// NewEventsAPIWriter creates a new EventsAPIWriter with a background sender.
func NewEventsAPIWriter(logger *zap.Logger, baseURL, userID, nodeID string) *EventsAPIWriter {
	ctx, cancel := context.WithCancel(context.Background())

	c := api.NewClient(api.ClientConfig{
		BaseURL: baseURL,
		Timeout: 5 * time.Second,
		UserID:  userID,
		NodeID:  nodeID,
	})

	w := &EventsAPIWriter{
		logger:     logger.With(zap.String("component", "events_api_writer")),
		client:     c,
		ctx:        ctx,
		cancel:     cancel,
		outgoingCh: make(chan *rtapi.LobbySessionStateFrame, 1000),
		stopped:    false,
	}

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
			if _, err := w.client.StoreSessionEvent(ctx, frame); err != nil {
				w.logger.Warn("Failed to send session event", zap.Error(err))
			}
			cancel()
		}
	}
}

// Context returns the writer context.
func (w *EventsAPIWriter) Context() context.Context { return w.ctx }

// WriteFrame enqueues a frame for sending to the events API.
func (w *EventsAPIWriter) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if w.stopped {
		return fmt.Errorf("events api writer is stopped")
	}

	// Skip frames without events
	if len(frame.Events) == 0 {
		return nil
	}

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
	w.logger.Info("Events API writer closed")
}

// IsStopped returns whether the writer is stopped.
func (w *EventsAPIWriter) IsStopped() bool { return w.stopped }
