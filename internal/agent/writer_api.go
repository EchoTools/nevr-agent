package agent

import (
	"context"
	"encoding/base64"
	"fmt"

	rtapi "github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/echotools/nevrcap/pkg/processing"
	nkrtapi "github.com/heroiclabs/nakama-common/rtapi"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

const (
	StreamModeLobbyTelemetry = 22
)

// StreamWriter implements FrameWriter interface and sends frame data to a Nakama stream
type StreamWriter struct {
	logger         *zap.Logger
	streamClient   *NakamaWebSocketClient
	frameProcessor *processing.Processor
	ctx            context.Context
	cancel         context.CancelFunc
	outgoingCh     chan *rtapi.LobbySessionStateFrame
	stopped        bool
}

// StreamFramePayload represents the JSON payload sent to the stream
type StreamFramePayload struct {
	Timestamp      int64  `json:"timestamp"`
	SessionData    []byte `json:"session_data"`
	PlayerBoneData []byte `json:"player_bone_data"`
}

// NewStreamWriter creates a new StreamWriter
func NewStreamWriter(logger *zap.Logger, httpURL, socketURL, httpKey, serverKey, username, password string) *StreamWriter {
	ctx, cancel := context.WithCancel(context.Background())

	frameProcessor := processing.New()
	streamClient := NewStreamClient(logger, httpURL, socketURL, httpKey, serverKey, username, password)

	outgoingCh := make(chan *rtapi.LobbySessionStateFrame, 1000) // Buffered channel for outgoing frames

	return &StreamWriter{
		logger:         logger.With(zap.String("component", "stream_writer")),
		streamClient:   streamClient,
		frameProcessor: frameProcessor,
		ctx:            ctx,
		cancel:         cancel,
		outgoingCh:     outgoingCh,
		stopped:        false,
	}
}

// Connect establishes the connection to the Nakama server
func (sw *StreamWriter) Connect() error {
	return sw.streamClient.Connect()
}

// Context returns the context for this writer
func (sw *StreamWriter) Context() context.Context {
	return sw.ctx
}

// WriteFrame sends frame data to the Nakama stream
func (sw *StreamWriter) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if sw.stopped {
		return fmt.Errorf("stream writer is stopped")
	}

	// Create payload with frame data
	payload := rtapi.Envelope{
		Message: &rtapi.Envelope_LobbySessionState{
			LobbySessionState: &rtapi.LobbySessionStateMessage{
				State: &rtapi.LobbySessionStateMessage_SessionState{
					SessionState: frame,
				},
			},
		},
	}

	data, err := proto.Marshal(&payload)
	if err != nil {
		return fmt.Errorf("failed to marshal frame payload: %w", err)
	}
	// Encode to base64 string
	encoded := base64.StdEncoding.EncodeToString(data)

	envelope := &nkrtapi.Envelope{
		Message: &nkrtapi.Envelope_StreamData{
			StreamData: &nkrtapi.StreamData{
				Stream: &nkrtapi.Stream{
					Mode:    StreamModeLobbyTelemetry,
					Subject: frame.GetSession().GetSessionId(),
				},
				Data: encoded,
			},
		},
	}
	// Send data to stream
	sw.streamClient.Send(envelope, false)

	sw.logger.Debug("Sent frame to stream",
		zap.Int("payload_size", len(data)))

	return nil
}

// Close closes the stream writer and connection
func (sw *StreamWriter) Close() {
	if sw.stopped {
		return
	}

	sw.stopped = true
	sw.cancel()

	if err := sw.streamClient.Close(); err != nil {
		sw.logger.Error("Failed to close stream client", zap.Error(err))
	}

	sw.logger.Info("Stream writer closed")
}

// IsStopped returns whether the writer has been stopped
func (sw *StreamWriter) IsStopped() bool {
	return sw.stopped
}
