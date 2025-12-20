package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	telemetry "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

// StreamHub manages subscriptions to match streams
type StreamHub struct {
	mu           sync.RWMutex
	matches      map[string]*matchStream
	storage      *StorageManager
	logger       Logger
	metrics      *Metrics
	maxFrameRate int
	upgrader     websocket.Upgrader
	playerLookup *PlayerLookupService
}

// matchStream represents a stream for a single match
type matchStream struct {
	matchID     string
	subscribers map[*streamSubscriber]struct{}
	frames      []*telemetry.LobbySessionStateFrame // Ring buffer for seeking
	frameIndex  map[uint32]int                      // Map frame index to buffer position
	mu          sync.RWMutex
	maxFrames   int
	startTime   time.Time
}

// streamSubscriber represents a WebSocket subscriber
type streamSubscriber struct {
	conn      *websocket.Conn
	matchID   string
	frameRate int
	send      chan []byte
	done      chan struct{}
	paused    bool
	seekFrame uint32
	mu        sync.Mutex
}

// StreamMessage represents a message sent to/from the stream
type StreamMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SeekRequest represents a seek request
type SeekRequest struct {
	Frame uint32 `json:"frame,omitempty"`
	Time  string `json:"time,omitempty"` // Format: "MM:SS" or "HH:MM:SS"
}

// StreamControl represents stream control commands
type StreamControl struct {
	Command   string `json:"command"` // play, pause, seek
	FrameRate int    `json:"framerate,omitempty"`
}

// NewStreamHub creates a new stream hub
func NewStreamHub(storage *StorageManager, logger Logger, metrics *Metrics, maxFrameRate int, playerLookup *PlayerLookupService) *StreamHub {
	return &StreamHub{
		matches:      make(map[string]*matchStream),
		storage:      storage,
		logger:       logger,
		metrics:      metrics,
		maxFrameRate: maxFrameRate,
		playerLookup: playerLookup,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024 * 64, // 64KB for frame data
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// RegisterRoutes registers the stream API routes
func (h *StreamHub) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/v3/stream/{matchId}", h.handleStreamConnection).Methods("GET")
	r.HandleFunc("/api/v3/stream/{matchId}/info", h.handleStreamInfo).Methods("GET")
}

// handleStreamConnection handles WebSocket connections for streaming
func (h *StreamHub) handleStreamConnection(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID := vars["matchId"]

	// Parse frame rate from query params
	frameRate := 30
	if fpsStr := r.URL.Query().Get("fps"); fpsStr != "" {
		if fps, err := strconv.Atoi(fpsStr); err == nil && fps > 0 {
			frameRate = fps
			if frameRate > h.maxFrameRate {
				frameRate = h.maxFrameRate
			}
		}
	}

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade websocket", "error", err)
		return
	}

	subscriber := &streamSubscriber{
		conn:      conn,
		matchID:   matchID,
		frameRate: frameRate,
		send:      make(chan []byte, 256),
		done:      make(chan struct{}),
	}

	// Subscribe to the match
	h.subscribe(matchID, subscriber)
	defer h.unsubscribe(matchID, subscriber)

	if h.metrics != nil {
		h.metrics.RecordWebSocketConnect()
		defer h.metrics.RecordWebSocketDisconnect()
	}

	// Start send and receive goroutines
	go subscriber.writePump(h.logger)
	subscriber.readPump(h)
}

// handleStreamInfo returns information about an available stream
func (h *StreamHub) handleStreamInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID := vars["matchId"]

	h.mu.RLock()
	stream, exists := h.matches[matchID]
	h.mu.RUnlock()

	if !exists {
		// Check if there's a stored file
		if h.storage != nil {
			if filePath, err := h.storage.GetMatchFile(matchID); err == nil {
				info := map[string]interface{}{
					"match_id": matchID,
					"status":   "completed",
					"file":     filePath,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(info)
				return
			}
		}
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	stream.mu.RLock()
	info := map[string]interface{}{
		"match_id":    matchID,
		"status":      "live",
		"subscribers": len(stream.subscribers),
		"frames":      len(stream.frames),
		"start_time":  stream.startTime,
	}
	stream.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// subscribe adds a subscriber to a match stream
func (h *StreamHub) subscribe(matchID string, sub *streamSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	stream, exists := h.matches[matchID]
	if !exists {
		stream = &matchStream{
			matchID:     matchID,
			subscribers: make(map[*streamSubscriber]struct{}),
			frames:      make([]*telemetry.LobbySessionStateFrame, 0, 1000),
			frameIndex:  make(map[uint32]int),
			maxFrames:   10000, // Keep last 10000 frames (~5-10 min at 30fps)
			startTime:   time.Now(),
		}
		h.matches[matchID] = stream
	}

	stream.mu.Lock()
	stream.subscribers[sub] = struct{}{}
	stream.mu.Unlock()

	h.logger.Info("subscriber joined stream", "match_id", matchID, "framerate", sub.frameRate)
}

// unsubscribe removes a subscriber from a match stream
func (h *StreamHub) unsubscribe(matchID string, sub *streamSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	stream, exists := h.matches[matchID]
	if !exists {
		return
	}

	stream.mu.Lock()
	delete(stream.subscribers, sub)
	subscriberCount := len(stream.subscribers)
	stream.mu.Unlock()

	// Clean up empty streams (but keep data for a while)
	if subscriberCount == 0 {
		h.logger.Info("stream has no subscribers", "match_id", matchID)
	}

	close(sub.done)
	h.logger.Info("subscriber left stream", "match_id", matchID)
}

// BroadcastFrame broadcasts a frame to all subscribers of a match
func (h *StreamHub) BroadcastFrame(matchID string, frame *telemetry.LobbySessionStateFrame) {
	h.mu.RLock()
	stream, exists := h.matches[matchID]
	h.mu.RUnlock()

	if !exists {
		// Create new stream for this match
		h.mu.Lock()
		stream = &matchStream{
			matchID:     matchID,
			subscribers: make(map[*streamSubscriber]struct{}),
			frames:      make([]*telemetry.LobbySessionStateFrame, 0, 1000),
			frameIndex:  make(map[uint32]int),
			maxFrames:   10000,
			startTime:   time.Now(),
		}
		h.matches[matchID] = stream
		h.mu.Unlock()
	}

	// Store frame for seeking
	stream.mu.Lock()
	bufferPos := len(stream.frames)
	if bufferPos >= stream.maxFrames {
		// Ring buffer: remove oldest frame
		oldFrame := stream.frames[0]
		delete(stream.frameIndex, oldFrame.GetFrameIndex())
		stream.frames = stream.frames[1:]
		bufferPos = len(stream.frames)
	}
	stream.frames = append(stream.frames, frame)
	stream.frameIndex[frame.GetFrameIndex()] = bufferPos

	// Get subscribers
	subs := make([]*streamSubscriber, 0, len(stream.subscribers))
	for sub := range stream.subscribers {
		subs = append(subs, sub)
	}
	stream.mu.Unlock()

	// Serialize frame once
	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: false,
	}
	frameBytes, err := marshaler.Marshal(frame)
	if err != nil {
		h.logger.Error("failed to marshal frame", "error", err)
		return
	}

	// Wrap in message
	msg := StreamMessage{
		Type:    "frame",
		Payload: frameBytes,
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal message", "error", err)
		return
	}

	// Send to all subscribers
	for _, sub := range subs {
		sub.mu.Lock()
		if !sub.paused {
			select {
			case sub.send <- msgBytes:
			default:
				// Channel full, skip this frame for this subscriber
			}
		}
		sub.mu.Unlock()
	}
}

// CloseMatch marks a match as complete
func (h *StreamHub) CloseMatch(matchID string) {
	h.mu.Lock()
	stream, exists := h.matches[matchID]
	h.mu.Unlock()

	if !exists {
		return
	}

	// Notify subscribers
	msg := StreamMessage{
		Type: "match_ended",
	}
	msgBytes, _ := json.Marshal(msg)

	stream.mu.RLock()
	for sub := range stream.subscribers {
		select {
		case sub.send <- msgBytes:
		default:
		}
	}
	stream.mu.RUnlock()

	h.logger.Info("match stream closed", "match_id", matchID)
}

// writePump sends messages to the WebSocket
func (s *streamSubscriber) writePump(logger Logger) {
	ticker := time.NewTicker(time.Second / time.Duration(s.frameRate))
	defer func() {
		ticker.Stop()
		s.conn.Close()
	}()

	for {
		select {
		case <-s.done:
			return
		case message, ok := <-s.send:
			if !ok {
				s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				logger.Debug("failed to write message", "error", err)
				return
			}
		case <-ticker.C:
			// Ping to keep connection alive
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump reads messages from the WebSocket
func (s *streamSubscriber) readPump(hub *StreamHub) {
	defer s.conn.Close()

	s.conn.SetReadLimit(64 * 1024) // 64KB
	s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				hub.logger.Debug("websocket read error", "error", err)
			}
			return
		}

		// Parse message
		var msg StreamMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			hub.logger.Debug("failed to parse message", "error", err)
			continue
		}

		switch msg.Type {
		case "control":
			s.handleControl(hub, msg.Payload)
		case "seek":
			s.handleSeek(hub, msg.Payload)
		}
	}
}

// handleControl handles stream control commands
func (s *streamSubscriber) handleControl(hub *StreamHub, payload json.RawMessage) {
	var ctrl StreamControl
	if err := json.Unmarshal(payload, &ctrl); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch ctrl.Command {
	case "pause":
		s.paused = true
	case "play":
		s.paused = false
	case "framerate":
		if ctrl.FrameRate > 0 && ctrl.FrameRate <= hub.maxFrameRate {
			s.frameRate = ctrl.FrameRate
		}
	}
}

// handleSeek handles seek requests
func (s *streamSubscriber) handleSeek(hub *StreamHub, payload json.RawMessage) {
	var seek SeekRequest
	if err := json.Unmarshal(payload, &seek); err != nil {
		return
	}

	hub.mu.RLock()
	stream, exists := hub.matches[s.matchID]
	hub.mu.RUnlock()

	if !exists {
		return
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	var targetFrame *telemetry.LobbySessionStateFrame

	if seek.Frame > 0 {
		// Seek by frame index
		if pos, ok := stream.frameIndex[seek.Frame]; ok {
			targetFrame = stream.frames[pos]
		}
	} else if seek.Time != "" {
		// Seek by time (TODO: implement time-based seeking)
		// For now, just use frame-based seeking
	}

	if targetFrame != nil {
		// Send the target frame
		marshaler := protojson.MarshalOptions{EmitUnpopulated: false}
		frameBytes, err := marshaler.Marshal(targetFrame)
		if err == nil {
			msg := StreamMessage{
				Type:    "frame",
				Payload: frameBytes,
			}
			msgBytes, _ := json.Marshal(msg)
			select {
			case s.send <- msgBytes:
			default:
			}
		}
	}
}

// ReplayMatch replays a stored match to a subscriber
func (h *StreamHub) ReplayMatch(ctx context.Context, matchID string, sub *streamSubscriber) error {
	if h.storage == nil {
		return fmt.Errorf("storage not available")
	}

	filePath, err := h.storage.GetMatchFile(matchID)
	if err != nil {
		return err
	}

	reader, err := codecs.NewNevrCapReader(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer reader.Close()

	// Skip header
	if _, err := reader.ReadHeader(); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	interval := time.Second / time.Duration(sub.frameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	marshaler := protojson.MarshalOptions{EmitUnpopulated: false}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sub.done:
			return nil
		case <-ticker.C:
			sub.mu.Lock()
			paused := sub.paused
			sub.mu.Unlock()

			if paused {
				continue
			}

			frame, err := reader.ReadFrame()
			if err != nil {
				if err == io.EOF {
					// Send end of stream message
					msg := StreamMessage{Type: "stream_ended"}
					msgBytes, _ := json.Marshal(msg)
					select {
					case sub.send <- msgBytes:
					default:
					}
					return nil
				}
				return fmt.Errorf("failed to read frame: %w", err)
			}

			frameBytes, err := marshaler.Marshal(frame)
			if err != nil {
				continue
			}

			msg := StreamMessage{
				Type:    "frame",
				Payload: frameBytes,
			}
			msgBytes, _ := json.Marshal(msg)
			select {
			case sub.send <- msgBytes:
			default:
				// Buffer full, skip frame
			}
		}
	}
}
