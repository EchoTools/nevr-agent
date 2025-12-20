package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	telemetry "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

var jsonMarshaler = &protojson.MarshalOptions{
	UseProtoNames:   false,
	UseEnumNumbers:  true,
	EmitUnpopulated: true,
	Indent:          "  ",
}

type ReplayServer struct {
	files    []string
	loop     bool
	bindAddr string

	mu           sync.RWMutex
	currentFrame *telemetry.LobbySessionStateFrame
	isPlaying    bool
	frameCount   int64
	startTime    time.Time
}

type FrameResponse struct {
	Timestamp      string                       `json:"timestamp"`
	SessionData    *apigame.SessionResponse     `json:"session_data"`
	PlayerBoneData *apigame.PlayerBonesResponse `json:"player_bone_data,omitempty"`
	FrameNumber    int64                        `json:"frame_number"`
	ElapsedTime    string                       `json:"elapsed_time"`
	IsPlaying      bool                         `json:"is_playing"`
}

func newReplayerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay [replay-file...]",
		Short: "Replay recorded sessions via HTTP server",
		Long: `The replay command starts an HTTP server that plays back recorded 
session data from .echoreplay files.`,
		Example: `  # Replay a single file
	  agent replay game.echoreplay

  # Replay multiple files in sequence
	  agent replay game1.echoreplay game2.echoreplay

  # Replay in loop mode
	  agent replay --loop game.echoreplay

  # Custom bind address
	  agent replay --bind 0.0.0.0:8080 game.echoreplay`,
		RunE: runReplayer,
		Args: cobra.MinimumNArgs(1),
	}

	// Replayer-specific flags
	cmd.Flags().String("bind", "127.0.0.1:6721", "Host:port to bind HTTP server to")
	cmd.Flags().Bool("loop", false, "Loop playback continuously")

	// Bind flags to viper
	viper.BindPFlags(cmd.Flags())

	return cmd
}

func runReplayer(cmd *cobra.Command, args []string) error {
	// Override config with command flags
	cfg.Replayer.BindAddress = viper.GetString("bind")
	cfg.Replayer.Loop = viper.GetBool("loop")
	cfg.Replayer.Files = args

	// Validate configuration
	if err := cfg.ValidateReplayerConfig(); err != nil {
		return err
	}

	logger.Info("Starting replayer",
		zap.String("bind_address", cfg.Replayer.BindAddress),
		zap.Bool("loop", cfg.Replayer.Loop),
		zap.Strings("files", cfg.Replayer.Files))

	server := &ReplayServer{
		files:    cfg.Replayer.Files,
		loop:     cfg.Replayer.Loop,
		bindAddr: cfg.Replayer.BindAddress,
	}

	// Start playback in background
	go server.playback()

	// Create a dedicated ServeMux instead of using the default one
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleRoot)
	mux.HandleFunc("/frame", server.handleFrame)
	mux.HandleFunc("/session", server.handleSession)
	mux.HandleFunc("/player_bones", server.handlePlayerBones)
	mux.HandleFunc("/status", server.handleStatus)

	logger.Info("Replay server started",
		zap.String("address", cfg.Replayer.BindAddress),
		zap.Strings("files", cfg.Replayer.Files),
		zap.Bool("loop", cfg.Replayer.Loop))
	logger.Info("Available endpoints",
		zap.String("GET /", "Current frame (HTML)"),
		zap.String("GET /frame", "Current frame data (JSON)"),
		zap.String("GET /session", "Current session data (JSON)"),
		zap.String("GET /player_bones", "Current player bone data (JSON)"),
		zap.String("GET /status", "Server status (JSON)"))

	if err := http.ListenAndServe(cfg.Replayer.BindAddress, mux); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func (rs *ReplayServer) playback() {
	for {
		for _, file := range rs.files {
			logger.Info("Playing file", zap.String("file", file))
			rs.mu.Lock()
			rs.isPlaying = true
			rs.frameCount = 0
			rs.startTime = time.Now()
			rs.mu.Unlock()

			if err := rs.playFile(file); err != nil {
				logger.Error("Error playing file", zap.String("file", file), zap.Error(err))
			}
		}

		rs.mu.Lock()
		rs.isPlaying = false
		rs.mu.Unlock()

		if !rs.loop {
			logger.Info("Playback finished")
			break
		}

		logger.Info("Looping playback...")
		time.Sleep(1 * time.Second)
	}
}

func (rs *ReplayServer) playFile(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".echoreplay":
		return rs.playEchoReplayFile(filename)
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}
}

func (rs *ReplayServer) playEchoReplayFile(filename string) error {
	reader, err := codecs.NewEchoReplayReader(filename)
	if err != nil {
		return fmt.Errorf("failed to open echo replay file: %w", err)
	}
	defer reader.Close()

	var lastTimestamp time.Time

	for reader.HasNext() {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Calculate delay for 1x playback speed
		if !lastTimestamp.IsZero() && frame.GetTimestamp() != nil {
			delay := frame.GetTimestamp().AsTime().Sub(lastTimestamp)
			if delay > 0 && delay < 10*time.Second { // Cap max delay
				time.Sleep(delay)
			}
		}
		if frame.GetTimestamp() != nil {
			lastTimestamp = frame.GetTimestamp().AsTime()
		}

		// Update current frame
		rs.mu.Lock()
		rs.currentFrame = frame
		rs.frameCount++
		rs.mu.Unlock()
	}

	return nil
}

func (rs *ReplayServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	frame := rs.currentFrame
	isPlaying := rs.isPlaying
	frameCount := rs.frameCount
	startTime := rs.startTime
	rs.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html")

	html := `<!DOCTYPE html>
<html>
<head>
    <title>Replay Server</title>
    <meta http-equiv="refresh" content="1">
    <style>
        body { font-family: monospace; margin: 20px; }
        .status { background: #f0f0f0; padding: 10px; margin-bottom: 20px; }
        .frame { background: #f8f8f8; padding: 10px; }
        pre { white-space: pre-wrap; word-wrap: break-word; }
    </style>
</head>
<body>
    <h1>Replay Server</h1>
    <div class="status">
        <strong>Status:</strong> %s<br>
        <strong>Frame:</strong> %d<br>
        <strong>Uptime:</strong> %s<br>
        <strong>Files:</strong> %v<br>
        <strong>Loop:</strong> %v
    </div>
    <div class="frame">
        <h2>Current Frame</h2>
        <pre>%s</pre>
    </div>
    <p><a href="/frame">JSON Endpoint</a> | <a href="/status">Status Endpoint</a></p>
</body>
</html>`

	status := "Stopped"
	if isPlaying {
		status = "Playing"
	}

	uptime := time.Since(startTime).Round(time.Second)

	frameJSON := "No frame data"
	if frame != nil {
		if response, err := rs.buildFrameResponse(frame, frameCount, startTime); err == nil {
			if jsonBytes, err := json.MarshalIndent(response, "", "  "); err == nil {
				frameJSON = string(jsonBytes)
			}
		}
	}

	fmt.Fprintf(w, html, status, frameCount, uptime, rs.files, rs.loop, frameJSON)
}

func (rs *ReplayServer) handleFrame(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	frame := rs.currentFrame
	frameCount := rs.frameCount
	startTime := rs.startTime
	rs.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if frame == nil {
		w.WriteHeader(http.StatusNoContent)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "No frame data available",
		})
		return
	}

	response, err := rs.buildFrameResponse(frame, frameCount, startTime)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
		return
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(response)
}

func (rs *ReplayServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	isPlaying := rs.isPlaying
	frameCount := rs.frameCount
	startTime := rs.startTime
	rs.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	status := map[string]any{
		"is_playing":   isPlaying,
		"frame_count":  frameCount,
		"uptime":       time.Since(startTime).String(),
		"files":        rs.files,
		"loop":         rs.loop,
		"bind_address": rs.bindAddr,
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(status)
}

func (rs *ReplayServer) handleSession(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	var frameData *apigame.SessionResponse
	if rs.currentFrame != nil {
		frameData = rs.currentFrame.GetSession()
	}
	rs.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if frameData == nil {
		w.WriteHeader(http.StatusNoContent)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "No frame data available",
		})
		return
	}

	data, err := jsonMarshaler.Marshal(frameData)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
		return
	}

	w.Write(data)
}

func (rs *ReplayServer) handlePlayerBones(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	var boneData *apigame.PlayerBonesResponse
	if rs.currentFrame != nil {
		boneData = rs.currentFrame.GetPlayerBones()
	}
	rs.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if boneData == nil {
		w.WriteHeader(http.StatusNoContent)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "No player bone data available",
		})
		return
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(boneData)
}

func (rs *ReplayServer) buildFrameResponse(frame *telemetry.LobbySessionStateFrame, frameCount int64, startTime time.Time) (*FrameResponse, error) {
	timestamp := ""
	if frame.GetTimestamp() != nil {
		timestamp = frame.GetTimestamp().AsTime().Format(time.RFC3339Nano)
	}

	response := &FrameResponse{
		SessionData:    frame.GetSession(),
		PlayerBoneData: frame.GetPlayerBones(),
		Timestamp:      timestamp,
		FrameNumber:    frameCount,
		ElapsedTime:    time.Since(startTime).String(),
		IsPlaying:      rs.isPlaying,
	}

	return response, nil
}
