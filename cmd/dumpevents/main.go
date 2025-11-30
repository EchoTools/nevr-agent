package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/echotools/nevrcap/pkg/codecs"
	"github.com/echotools/nevrcap/pkg/events"
)

func main() {
	// Parse command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <replay-file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s <replay-file> [output-format]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nSupported file formats:\n")
		fmt.Fprintf(os.Stderr, "  .echoreplay - EchoVR replay format\n")
		fmt.Fprintf(os.Stderr, "  .nevrcap    - NEVR capture format\n")
		fmt.Fprintf(os.Stderr, "\nOutput formats:\n")
		fmt.Fprintf(os.Stderr, "  json     - JSON format (default)\n")
		fmt.Fprintf(os.Stderr, "  text     - Human-readable text format\n")
		fmt.Fprintf(os.Stderr, "  summary  - Event summary statistics\n")
		os.Exit(1)
	}

	filename := os.Args[1]
	outputFormat := "json"
	if len(os.Args) > 2 {
		outputFormat = os.Args[2]
	}

	// Validate file exists and has correct extension
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", filename)
	}

	ext := filepath.Ext(filename)
	if ext != ".echoreplay" && ext != ".nevrcap" {
		log.Fatalf("File must have .echoreplay or .nevrcap extension, got: %s", ext)
	}

	// Process the file and output events
	if err := processEchoReplayFile(filename, outputFormat); err != nil {
		log.Fatalf("Failed to process file: %v", err)
	}
}

// frameReader is a common interface for reading frames from different file formats
type frameReader interface {
	ReadFrameTo(frame *rtapi.LobbySessionStateFrame) (bool, error)
	Close() error
}

func processEchoReplayFile(filename, outputFormat string) error {
	// Open the replay file based on extension
	var reader frameReader
	var err error

	ext := filepath.Ext(filename)
	switch ext {
	case ".echoreplay":
		reader, err = codecs.NewEchoReplayReader(filename)
	case ".nevrcap":
		reader, err = codecs.NewNevrCapReader(filename)
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}

	if err != nil {
		return fmt.Errorf("failed to open replay file: %w", err)
	}
	defer reader.Close()

	// Create event detector
	detector := events.New()

	// Statistics for summary mode
	eventStats := make(map[string]int)
	frameCount := 0
	var startTime, endTime time.Time

	var (
		frameMu         sync.RWMutex
		currentFrame    *rtapi.LobbySessionStateFrame
		eventsWG        sync.WaitGroup
		eventErrChan    = make(chan error, 1)
		eventHandlerErr error
	)

	handleEvent := func(event *rtapi.LobbySessionEvent, frame *rtapi.LobbySessionStateFrame) error {
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

	eventsWG.Go(func() {
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
	})

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
	var parseDuration int64 = 0
	var cycleTime time.Time
	for {
		if err := checkEventHandlerErr(); err != nil {
			return err
		}

		frame := &rtapi.LobbySessionStateFrame{}
		ok, err = reader.ReadFrameTo(frame)
		if err != nil || !ok {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		frameCount++

		parseDuration += time.Since(cycleTime).Nanoseconds()

		// Track timing for summary
		if frameCount == 1 {
			startTime = frame.Timestamp.AsTime()
		}
		endTime = frame.Timestamp.AsTime()

		frameMu.Lock()
		currentFrame = frame
		frameMu.Unlock()

		// Queue frame for async detection
		detector.ProcessFrame(frame)
	}

	if frameCount > 0 {
		fmt.Println(parseDuration / int64(frameCount))
	}

	stopDetector()

	if err := checkEventHandlerErr(); err != nil {
		return err
	}

	// Output summary if requested
	if outputFormat == "summary" {
		outputSummary(eventStats, frameCount, startTime, endTime, filename)
	}

	return nil
}

func outputEventJSON(event *rtapi.LobbySessionEvent, frame *rtapi.LobbySessionStateFrame) error {
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

func outputEventText(event *rtapi.LobbySessionEvent, frame *rtapi.LobbySessionStateFrame) {
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
	case *rtapi.LobbySessionEvent_PlayerJoined:
		fmt.Printf(" - Player: %s (Slot %d)",
			payload.PlayerJoined.Player.DisplayName,
			payload.PlayerJoined.Player.SlotNumber)
	case *rtapi.LobbySessionEvent_PlayerLeft:
		fmt.Printf(" - Player: %s (Slot %d)",
			payload.PlayerLeft.DisplayName,
			payload.PlayerLeft.PlayerSlot)
	case *rtapi.LobbySessionEvent_GoalScored:
		if payload.GoalScored.ScoreDetails != nil {
			fmt.Printf(" - Goal by player %s",
				payload.GoalScored.ScoreDetails.PersonScored)
		}
	case *rtapi.LobbySessionEvent_RoundStarted:
		fmt.Printf(" - Round started")
	case *rtapi.LobbySessionEvent_RoundEnded:
		fmt.Printf(" - Round ended, Winner: %s",
			payload.RoundEnded.WinningTeam.String())
	case *rtapi.LobbySessionEvent_MatchEnded:
		fmt.Printf(" - Match ended, Winner: %s",
			payload.MatchEnded.WinningTeam.String())
	case *rtapi.LobbySessionEvent_ScoreboardUpdated:
		fmt.Printf(" - Score: Blue %d-%d Orange",
			payload.ScoreboardUpdated.BluePoints,
			payload.ScoreboardUpdated.OrangePoints)
	case *rtapi.LobbySessionEvent_DiscPossessionChanged:
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

func updateEventStats(event *rtapi.LobbySessionEvent, stats map[string]int) {
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

func getEventTypeName(event *rtapi.LobbySessionEvent) string {
	switch event.Event.(type) {
	case *rtapi.LobbySessionEvent_RoundStarted:
		return "RoundStarted"
	case *rtapi.LobbySessionEvent_RoundPaused:
		return "RoundPaused"
	case *rtapi.LobbySessionEvent_RoundUnpaused:
		return "RoundUnpaused"
	case *rtapi.LobbySessionEvent_RoundEnded:
		return "RoundEnded"
	case *rtapi.LobbySessionEvent_MatchEnded:
		return "MatchEnded"
	case *rtapi.LobbySessionEvent_ScoreboardUpdated:
		return "ScoreboardUpdated"
	case *rtapi.LobbySessionEvent_PlayerJoined:
		return "PlayerJoined"
	case *rtapi.LobbySessionEvent_PlayerLeft:
		return "PlayerLeft"
	case *rtapi.LobbySessionEvent_PlayerSwitchedTeam:
		return "PlayerSwitchedTeam"
	case *rtapi.LobbySessionEvent_EmotePlayed:
		return "EmotePlayed"
	case *rtapi.LobbySessionEvent_DiscPossessionChanged:
		return "DiscPossessionChanged"
	case *rtapi.LobbySessionEvent_DiscThrown:
		return "DiscThrown"
	case *rtapi.LobbySessionEvent_DiscCaught:
		return "DiscCaught"
	case *rtapi.LobbySessionEvent_GoalScored:
		return "GoalScored"
	case *rtapi.LobbySessionEvent_PlayerSave:
		return "PlayerSave"
	case *rtapi.LobbySessionEvent_PlayerStun:
		return "PlayerStun"
	case *rtapi.LobbySessionEvent_PlayerPass:
		return "PlayerPass"
	case *rtapi.LobbySessionEvent_PlayerSteal:
		return "PlayerSteal"
	case *rtapi.LobbySessionEvent_PlayerBlock:
		return "PlayerBlock"
	case *rtapi.LobbySessionEvent_PlayerInterception:
		return "PlayerInterception"
	case *rtapi.LobbySessionEvent_PlayerAssist:
		return "PlayerAssist"
	case *rtapi.LobbySessionEvent_PlayerShotTaken:
		return "PlayerShotTaken"
	default:
		return "Unknown"
	}
}
