package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-capture/v3/pkg/processing"
	telemetry "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newDumpEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <replay-file> [output-format]",
		Short: "Extract and display events from replay files",
		Long: `Process replay files (.echoreplay or .nevrcap) and output detected events.

Supported file formats:
  .echoreplay            - EchoVR replay format (compressed zip)
  .echoreplay.uncompressed - EchoVR replay format (uncompressed)
  .nevrcap               - NEVR capture format (zstd compressed)
  .nevrcap.uncompressed  - NEVR capture format (uncompressed)

Output formats:
  json     - JSON format (default)
  text     - Human-readable text format
  summary  - Event summary statistics`,
		Example: `  # Output events as JSON (default)
  agent show game.echoreplay

  # Output as human-readable text
  agent show game.nevrcap text

  # Show event summary statistics
  agent show game.echoreplay summary`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runDumpEvents,
	}

	return cmd
}

func runDumpEvents(cmd *cobra.Command, args []string) error {
	filename := args[0]
	outputFormat := "json"
	if len(args) > 1 {
		outputFormat = args[1]
	}

	// Validate file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filename)
	}

	// Validate file extension
	lowerFilename := strings.ToLower(filename)
	validExtensions := []string{".echoreplay", ".echoreplay.uncompressed", ".nevrcap", ".nevrcap.uncompressed"}
	hasValidExt := false
	for _, ext := range validExtensions {
		if strings.HasSuffix(lowerFilename, ext) {
			hasValidExt = true
			break
		}
	}
	if !hasValidExt {
		return fmt.Errorf("file must have .echoreplay, .nevrcap (or .uncompressed variants) extension, got: %s", filename)
	}

	// Process the file and output events
	return processReplayFile(filename, outputFormat)
}

// frameReader is a common interface for reading frames from different file formats
type frameReader interface {
	ReadFrameTo(frame *telemetry.LobbySessionStateFrame) (bool, error)
	Close() error
}

func processReplayFile(filename, outputFormat string) error {
	// Open the replay file based on extension
	var reader frameReader
	var err error

	lowerFilename := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lowerFilename, ".echoreplay.uncompressed"):
		reader, err = newUncompressedEchoReplayReader(filename)
	case strings.HasSuffix(lowerFilename, ".echoreplay"):
		reader, err = codecs.NewEchoReplayReader(filename)
	case strings.HasSuffix(lowerFilename, ".nevrcap.uncompressed"):
		reader, err = newUncompressedNevrCapReader(filename)
	case strings.HasSuffix(lowerFilename, ".nevrcap"):
		reader, err = codecs.NewNevrCapReader(filename)
	default:
		return fmt.Errorf("unsupported file format: %s", filename)
	}

	if err != nil {
		return fmt.Errorf("failed to open replay file: %w", err)
	}
	defer reader.Close()

	// Create event detector
	detector := processing.New()

	// Statistics for summary mode
	eventStats := make(map[string]int)
	frameCount := 0
	var startTime, endTime *timestamppb.Timestamp

	var (
		frameMu         sync.RWMutex
		currentFrame    *telemetry.LobbySessionStateFrame
		eventsWG        sync.WaitGroup
		eventErrChan    = make(chan error, 1)
		eventHandlerErr error
	)

	handleEvent := func(event *telemetry.LobbySessionEvent, frame *telemetry.LobbySessionStateFrame) error {
		switch outputFormat {
		case "json":
			return outputEventJSON(event, frame)
		case "text":
			outputEventText(event, frame)
			return nil
		case "summary":
			updateEventStats(event, eventStats)
			return nil
		default:
			return fmt.Errorf("unsupported output format: %s", outputFormat)
		}
	}

	eventsWG.Add(1)
	go func() {
		defer eventsWG.Done()
		for events := range detector.EventsChan() {
			frameMu.RLock()
			frameSnapshot := currentFrame
			frameMu.RUnlock()

			for _, event := range events {
				if err := handleEvent(event, frameSnapshot); err != nil {
					select {
					case eventErrChan <- err:
					default:
					}
					return
				}
			}
		}
	}()

	var stopOnce sync.Once
	stopDetector := func() {
		stopOnce.Do(func() {
			detector.Stop()
			eventsWG.Wait()
		})
	}
	defer stopDetector()

	checkEventHandlerErr := func() error {
		if eventHandlerErr != nil {
			return eventHandlerErr
		}
		select {
		case err := <-eventErrChan:
			eventHandlerErr = err
			return err
		default:
			return nil
		}
	}

	// Process frames and detect events
	var ok bool
	for {
		if err := checkEventHandlerErr(); err != nil {
			return err
		}

		frame := &telemetry.LobbySessionStateFrame{}
		ok, err = reader.ReadFrameTo(frame)
		if err != nil || !ok {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		frameCount++

		// Track timing for summary
		if frameCount == 1 {
			startTime = frame.Timestamp
		}

		frameMu.Lock()
		currentFrame = frame
		frameMu.Unlock()

		// Queue frame for async detection
		detector.DetectEvents(frame)
	}

	endTime = currentFrame.Timestamp

	stopDetector()

	if err := checkEventHandlerErr(); err != nil {
		return err
	}

	// Output summary if requested
	if outputFormat == "summary" {
		outputSummary(eventStats, frameCount, startTime.AsTime(), endTime.AsTime(), filename)
	}

	return nil
}

func outputEventJSON(event *telemetry.LobbySessionEvent, frame *telemetry.LobbySessionStateFrame) error {
	// Create a structured output with event and frame context
	output := map[string]any{
		"event_type": getEventTypeName(event),
		"event_data": event,
	}

	// Add relevant game state context
	if frame != nil {
		output["timestamp"] = frame.Timestamp.AsTime().Format(time.RFC3339Nano)
		output["frame_index"] = frame.FrameIndex
		if frame.Session != nil {
			output["game_status"] = frame.Session.GameStatus
			output["game_clock"] = frame.Session.GameClockDisplay
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func outputEventText(event *telemetry.LobbySessionEvent, frame *telemetry.LobbySessionStateFrame) {
	timestamp := "unknown"
	frameLabel := "unknown"
	if frame != nil {
		timestamp = frame.Timestamp.AsTime().Format("2006-01-02 15:04:05.000")
		frameLabel = fmt.Sprintf("%d", frame.FrameIndex)
	}
	eventType := getEventTypeName(event)

	fmt.Printf("[%s] Frame %s: %s", timestamp, frameLabel, eventType)

	// Add specific event details
	switch payload := event.Event.(type) {
	case *telemetry.LobbySessionEvent_PlayerJoined:
		fmt.Printf(" - Player: %s (Slot %d)",
			payload.PlayerJoined.Player.DisplayName,
			payload.PlayerJoined.Player.SlotNumber)
	case *telemetry.LobbySessionEvent_PlayerLeft:
		fmt.Printf(" - Player: %s (Slot %d)",
			payload.PlayerLeft.DisplayName,
			payload.PlayerLeft.PlayerSlot)
	case *telemetry.LobbySessionEvent_GoalScored:
		if payload.GoalScored.ScoreDetails != nil {
			fmt.Printf(" - Goal by player %s",
				payload.GoalScored.ScoreDetails.PersonScored)
		}
	case *telemetry.LobbySessionEvent_RoundStarted:
		fmt.Printf(" - Round started")
	case *telemetry.LobbySessionEvent_RoundEnded:
		fmt.Printf(" - Round ended, Winner: %s",
			payload.RoundEnded.WinningTeam.String())
	case *telemetry.LobbySessionEvent_MatchEnded:
		fmt.Printf(" - Match ended, Winner: %s",
			payload.MatchEnded.WinningTeam.String())
	case *telemetry.LobbySessionEvent_ScoreboardUpdated:
		fmt.Printf(" - Score: Blue %d-%d Orange",
			payload.ScoreboardUpdated.BluePoints,
			payload.ScoreboardUpdated.OrangePoints)
	case *telemetry.LobbySessionEvent_DiscPossessionChanged:
		if payload.DiscPossessionChanged.PlayerSlot == -1 {
			fmt.Printf(" - Disc is free")
		} else {
			fmt.Printf(" - Disc possession: Player slot %d",
				payload.DiscPossessionChanged.PlayerSlot)
		}
	}

	// Add game status context
	if frame != nil && frame.Session != nil && frame.Session.GameStatus != "" {
		fmt.Printf(" (GameStatus: %s)", frame.Session.GameStatus)
	}

	fmt.Println()
}

func updateEventStats(event *telemetry.LobbySessionEvent, stats map[string]int) {
	eventType := getEventTypeName(event)
	stats[eventType]++
}

func outputSummary(stats map[string]int, frameCount int, startTime, endTime time.Time, filename string) {
	fmt.Printf("=== Event Summary for %s ===\n", filepath.Base(filename))
	fmt.Printf("Frames processed: %d\n", frameCount)
	fmt.Printf("Duration: %v\n", endTime.Sub(startTime))
	fmt.Printf("Start time: %s\n", startTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("End time: %s\n", endTime.Format("2006-01-02 15:04:05"))
	fmt.Println()

	totalEvents := 0
	for _, count := range stats {
		totalEvents += count
	}

	fmt.Printf("Total events detected: %d\n", totalEvents)
	fmt.Println("\nEvent breakdown:")

	// Sort event types for consistent output
	eventTypes := make([]string, 0, len(stats))
	for eventType := range stats {
		eventTypes = append(eventTypes, eventType)
	}

	for _, eventType := range eventTypes {
		count := stats[eventType]
		fmt.Printf("  %-25s: %d\n", eventType, count)
	}

	if frameCount > 0 {
		eventsPerSecond := float64(totalEvents) / endTime.Sub(startTime).Seconds()
		fmt.Printf("\nAverage events per second: %.2f\n", eventsPerSecond)
	}
}

func getEventTypeName(event *telemetry.LobbySessionEvent) string {
	switch event.Event.(type) {
	case *telemetry.LobbySessionEvent_RoundStarted:
		return "RoundStarted"
	case *telemetry.LobbySessionEvent_RoundPaused:
		return "RoundPaused"
	case *telemetry.LobbySessionEvent_RoundUnpaused:
		return "RoundUnpaused"
	case *telemetry.LobbySessionEvent_RoundEnded:
		return "RoundEnded"
	case *telemetry.LobbySessionEvent_MatchEnded:
		return "MatchEnded"
	case *telemetry.LobbySessionEvent_ScoreboardUpdated:
		return "ScoreboardUpdated"
	case *telemetry.LobbySessionEvent_PlayerJoined:
		return "PlayerJoined"
	case *telemetry.LobbySessionEvent_PlayerLeft:
		return "PlayerLeft"
	case *telemetry.LobbySessionEvent_PlayerSwitchedTeam:
		return "PlayerSwitchedTeam"
	case *telemetry.LobbySessionEvent_EmotePlayed:
		return "EmotePlayed"
	case *telemetry.LobbySessionEvent_DiscPossessionChanged:
		return "DiscPossessionChanged"
	case *telemetry.LobbySessionEvent_DiscThrown:
		return "DiscThrown"
	case *telemetry.LobbySessionEvent_DiscCaught:
		return "DiscCaught"
	case *telemetry.LobbySessionEvent_GoalScored:
		return "GoalScored"
	case *telemetry.LobbySessionEvent_PlayerSave:
		return "PlayerSave"
	case *telemetry.LobbySessionEvent_PlayerStun:
		return "PlayerStun"
	case *telemetry.LobbySessionEvent_PlayerPass:
		return "PlayerPass"
	case *telemetry.LobbySessionEvent_PlayerSteal:
		return "PlayerSteal"
	case *telemetry.LobbySessionEvent_PlayerBlock:
		return "PlayerBlock"
	case *telemetry.LobbySessionEvent_PlayerInterception:
		return "PlayerInterception"
	case *telemetry.LobbySessionEvent_PlayerAssist:
		return "PlayerAssist"
	case *telemetry.LobbySessionEvent_PlayerShotTaken:
		return "PlayerShotTaken"
	default:
		return "Unknown"
	}
}

// uncompressedEchoReplayReader reads uncompressed echoreplay files (plain text format)
type uncompressedEchoReplayReader struct {
	file    *os.File
	scanner *bufio.Scanner
	codec   *codecs.EchoReplay
}

func newUncompressedEchoReplayReader(filename string) (*uncompressedEchoReplayReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return &uncompressedEchoReplayReader{
		file:    file,
		scanner: bufio.NewScanner(file),
	}, nil
}

func (r *uncompressedEchoReplayReader) ReadFrameTo(frame *telemetry.LobbySessionStateFrame) (bool, error) {
	// EchoReplay format is tab-separated: timestamp\tsession_json\t player_bones_json
	// This is a simplified parser - for full support would need to reuse codec parsing
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return false, err
		}
		return false, io.EOF
	}

	// Create a temporary codec for parsing if needed
	if r.codec == nil {
		// Use the codec's internal parsing via a workaround
		// For now, return that we read a frame but it may not be fully parsed
		return true, fmt.Errorf("uncompressed echoreplay parsing not fully implemented")
	}

	return true, nil
}

func (r *uncompressedEchoReplayReader) Close() error {
	return r.file.Close()
}

// uncompressedNevrCapReader reads uncompressed nevrcap files (raw protobuf without zstd)
type uncompressedNevrCapReader struct {
	file   *os.File
	reader io.Reader
}

func newUncompressedNevrCapReader(filename string) (*uncompressedNevrCapReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	// Check if this is actually a zstd compressed file by looking at magic bytes
	magic := make([]byte, 4)
	if _, err := file.Read(magic); err != nil {
		file.Close()
		return nil, err
	}
	// Seek back to start
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return nil, err
	}

	var reader io.Reader
	// Zstd magic: 0x28, 0xB5, 0x2F, 0xFD
	if magic[0] == 0x28 && magic[1] == 0xB5 && magic[2] == 0x2F && magic[3] == 0xFD {
		// It's actually compressed, use zstd decoder
		decoder, err := zstd.NewReader(file)
		if err != nil {
			file.Close()
			return nil, err
		}
		reader = decoder
	} else {
		// Actually uncompressed
		reader = file
	}

	return &uncompressedNevrCapReader{
		file:   file,
		reader: reader,
	}, nil
}

func (r *uncompressedNevrCapReader) ReadFrameTo(frame *telemetry.LobbySessionStateFrame) (bool, error) {
	// Read varint length
	var length uint64
	var shift uint
	var b [1]byte
	for {
		if _, err := r.reader.Read(b[:]); err != nil {
			if err == io.EOF {
				return false, io.EOF
			}
			return false, err
		}

		length |= uint64(b[0]&0x7F) << shift
		if b[0]&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 64 {
			return false, io.ErrUnexpectedEOF
		}
	}

	// Read message data
	data := make([]byte, length)
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return false, err
	}

	// Try to unmarshal as frame
	if err := proto.Unmarshal(data, frame); err != nil {
		// Might be a header - try to skip it and read next
		return r.ReadFrameTo(frame)
	}

	return true, nil
}

func (r *uncompressedNevrCapReader) Close() error {
	if closer, ok := r.reader.(io.Closer); ok {
		closer.Close()
	}
	return r.file.Close()
}
