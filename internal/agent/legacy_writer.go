package agent

import (
	"errors"
	"os"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// LegacyWriter writes v1 telemetry data in the legacy nevrcap format:
// zstd-compressed, varint-length-delimited protobuf messages.
type LegacyWriter struct {
	file    *os.File
	encoder *zstd.Encoder
}

// NewLegacyWriter creates a writer for legacy .nevrcap files.
func NewLegacyWriter(filename string) (*LegacyWriter, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	encoder, err := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return &LegacyWriter{
		file:    file,
		encoder: encoder,
	}, nil
}

// WriteHeader writes a telemetry header as a varint-delimited protobuf message.
func (w *LegacyWriter) WriteHeader(header *telemetry.TelemetryHeader) error {
	data, err := proto.Marshal(header)
	if err != nil {
		return err
	}
	return w.writeDelimitedMessage(data)
}

// WriteFrame writes a telemetry frame as a varint-delimited protobuf message.
func (w *LegacyWriter) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return w.writeDelimitedMessage(data)
}

func (w *LegacyWriter) writeDelimitedMessage(data []byte) error {
	var buf [10]byte
	length := uint64(len(data))
	i := 0
	for length >= 0x80 {
		buf[i] = byte(length) | 0x80
		length >>= 7
		i++
	}
	buf[i] = byte(length)
	i++

	if _, err := w.encoder.Write(buf[:i]); err != nil {
		return err
	}
	_, err := w.encoder.Write(data)
	return err
}

// Close closes the zstd encoder and underlying file.
func (w *LegacyWriter) Close() error {
	var encErr error
	if w.encoder != nil {
		encErr = w.encoder.Close()
	}
	var fileErr error
	if w.file != nil {
		fileErr = w.file.Close()
	}
	return errors.Join(encErr, fileErr)
}
