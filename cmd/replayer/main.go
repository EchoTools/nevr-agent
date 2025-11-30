package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/echotools/nevrcap/pkg/codecs"
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
	currentFrame *rtapi.LobbySessionStateFrame
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

func main() {
	var (
		loop     = flag.Bool("loop", false, "Loop playback continuously")
		bindAddr = flag.String("bind", "127.0.0.1:6721", "Host:port to bind HTTP server to")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <replay-file> [replay-file...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nReplay files can be .echoreplay (zip) or .rtapi (protobuf) format.\n")
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	files := flag.Args()
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			log.Fatalf("File does not exist: %s", file)
		}
	}

	server := &ReplayServer{
		files:    files,
		loop:     *loop,
		bindAddr: *bindAddr,
	}

	// Start playback in background
	go server.playback()

	// Setup HTTP handlers
	http.HandleFunc("/", server.handleRoot)
	http.HandleFunc("/frame", server.handleFrame)
	http.HandleFunc("/session", server.handleSession)
	http.HandleFunc("/player_bones", server.handlePlayerBones)
	http.HandleFunc("/status", server.handleStatus)

	log.Printf("Starting replay server on %s", *bindAddr)
	log.Printf("Files: %v", files)
	log.Printf("Loop: %v", *loop)
	log.Printf("Endpoints:")
	log.Printf("  GET /        - Current frame (HTML)")
	log.Printf("  GET /frame    - Current frame data (JSON)")
	log.Printf("  GET /session   - Current session data from frame (JSON)")
	log.Printf("  GET /player_bones - Current player bone data from frame (JSON)")
	log.Printf("  GET /status  - Server status (JSON)")

	if err := http.ListenAndServe(*bindAddr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func (rs *ReplayServer) playback() {
	for {
		for _, file := range rs.files {
			log.Printf("Playing file: %s", file)
			rs.mu.Lock()
			rs.isPlaying = true
			rs.frameCount = 0
			rs.startTime = time.Now()
			rs.mu.Unlock()

			if err := rs.playFile(file); err != nil {
				log.Printf("Error playing file %s: %v", file, err)
			}
		}

		rs.mu.Lock()
		rs.isPlaying = false
		rs.mu.Unlock()

		if !rs.loop {
			log.Printf("Playback finished")
			break
		}

		log.Printf("Looping playback...")
		time.Sleep(1 * time.Second)
	}
}

func (rs *ReplayServer) playFile(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".echoreplay":
		return rs.playEchoReplayFile(filename)
	case ".rtapi":
		return fmt.Errorf("not implemented")
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

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Calculate delay for 1x playback speed
		if !lastTimestamp.IsZero() {
			delay := frame.GetTimestamp().AsTime().Sub(lastTimestamp)
			if delay > 0 && delay < 10*time.Second { // Cap max delay
				time.Sleep(delay)
			}
		}
		lastTimestamp = frame.GetTimestamp().AsTime()

		// Update current frame
		rs.mu.Lock()
		rs.currentFrame = frame
		rs.frameCount++
		rs.mu.Unlock()
	}

	return nil
}

func (rs *ReplayServer) playNevrCapFile(filename string) error {
	reader, err := codecs.NewEchoReplayReader(filename)
	if err != nil {
		return fmt.Errorf("failed to open rtapi file: %w", err)
	}
	defer reader.Close()

	var lastTimestamp time.Time

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Calculate delay for 1x playback speed
		if !lastTimestamp.IsZero() {
			delay := frame.Timestamp.AsTime().Sub(lastTimestamp)
			if delay > 0 && delay < 10*time.Second { // Cap max delay
				time.Sleep(delay)
			}
		}
		lastTimestamp = frame.Timestamp.AsTime()

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
	var frameData = rs.currentFrame.GetSession()
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
	}

	w.Write(data)
}

func (rs *ReplayServer) handlePlayerBones(w http.ResponseWriter, r *http.Request) {
	rs.mu.RLock()
	var boneData = rs.currentFrame.GetPlayerBones()
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

func (rs *ReplayServer) buildFrameResponse(frame *rtapi.LobbySessionStateFrame, frameCount int64, startTime time.Time) (*FrameResponse, error) {
	response := &FrameResponse{
		SessionData:    frame.GetSession(),
		PlayerBoneData: frame.GetPlayerBones(),
		Timestamp:      frame.GetTimestamp().AsTime().Format(time.RFC3339Nano),
		FrameNumber:    frameCount,
		ElapsedTime:    time.Since(startTime).String(),
		IsPlaying:      rs.isPlaying,
	}

	return response, nil
}
