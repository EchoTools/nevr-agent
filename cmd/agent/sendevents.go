package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/api"
	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	"github.com/echotools/nevr-capture/v3/pkg/events"
	"github.com/echotools/nevr-capture/v3/pkg/processing"
	telemetry "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func newSendEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push <replay-file>",
		Short: "Extract events from replay files and send them to the events API",
		Long: `Process replay files (.echoreplay or .nevrcap), detect events, 
and send them to the configured events API endpoint.

This is useful for:
  - Reprocessing old recordings to send events to the API
  - Testing event detection and API integration
  - Backfilling event data from existing recordings

Supported file formats:
  .echoreplay            - EchoVR replay format (compressed zip)
  .echoreplay.uncompressed - EchoVR replay format (uncompressed)
  .nevrcap               - NEVR capture format (zstd compressed)
  .nevrcap.uncompressed  - NEVR capture format (uncompressed)`,
		Example: `  # Send events from a replay file to the default events API
  agent push game.echoreplay --events-url https://g.echovrce.com/lobby-session-events

  # Send events with authentication
  agent push game.nevrcap --events-url https://api.example.com/events --token mytoken

  # Send events at a specific rate (frames per second)
  agent push game.echoreplay --rate 60 --events-url https://api.example.com/events

  # Dry-run mode - detect events without sending
  agent push game.nevrcap --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runSendEvents,
	}

	// Command-specific flags
	cmd.Flags().String("events-url", "http://localhost:8081", "Events API endpoint URL")
	cmd.Flags().String("token", "", "JWT token for API authentication")
	cmd.Flags().Float64("rate", 0, "Playback rate in frames per second (0 = as fast as possible)")
	cmd.Flags().Bool("dry-run", false, "Detect events without sending them to the API")
	cmd.Flags().Bool("verbose", false, "Print detailed information about each event")

	// Bind flags to viper so they can be overridden by config file and env vars
	viper.BindPFlag("sendevents.events_url", cmd.Flags().Lookup("events-url"))
	viper.BindPFlag("sendevents.token", cmd.Flags().Lookup("token"))
	viper.BindPFlag("sendevents.rate", cmd.Flags().Lookup("rate"))
	viper.BindPFlag("sendevents.dry_run", cmd.Flags().Lookup("dry-run"))
	viper.BindPFlag("sendevents.verbose", cmd.Flags().Lookup("verbose"))

	return cmd
}

func runSendEvents(cmd *cobra.Command, args []string) error {
	filename := args[0]

	// Get values from viper (which merges config file, env vars, and flags)
	// Flags take precedence when explicitly set
	eventsURL := viper.GetString("sendevents.events_url")
	token := viper.GetString("sendevents.token")
	rate := viper.GetFloat64("sendevents.rate")
	dryRun := viper.GetBool("sendevents.dry_run")
	verbose := viper.GetBool("sendevents.verbose")

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

	// Create API client (unless dry-run)
	var client *api.Client
	if !dryRun {
		if eventsURL == "" {
			return fmt.Errorf("events-url is required (use --dry-run to skip API calls)")
		}
		client = api.NewClient(api.ClientConfig{
			BaseURL:  eventsURL,
			Timeout:  10 * time.Second,
			JWTToken: token,
		})
	}

	return processSendEvents(filename, client, rate, dryRun, verbose)
}

func processSendEvents(filename string, client *api.Client, rate float64, dryRun, verbose bool) error {
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

	// Create event detector with synchronous processing
	detector := processing.NewWithDetector(events.New(events.WithSynchronousProcessing()))

	// Statistics
	frameCount := 0
	eventCount := 0
	eventsSent := 0
	var startTime, endTime time.Time

	// Rate limiting
	var ticker *time.Ticker
	if rate > 0 {
		ticker = time.NewTicker(time.Duration(float64(time.Second) / rate))
		defer ticker.Stop()
	}

	logger.Info("Starting event extraction and sending",
		zap.String("file", filename),
		zap.String("events_url", func() string {
			if dryRun {
				return "(dry-run)"
			}
			if client != nil {
				return client.GetJWTToken() // This is a workaround, ideally we'd expose the URL
			}
			return ""
		}()),
		zap.Float64("rate", rate),
		zap.Bool("dry_run", dryRun))

	// Process frames
	for {
		// Rate limiting
		if ticker != nil {
			<-ticker.C
		}

		frame := &telemetry.LobbySessionStateFrame{}
		ok, err := reader.ReadFrameTo(frame)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}
		if !ok {
			break
		}

		frameCount++

		// Track timing
		if frameCount == 1 {
			startTime = frame.Timestamp.AsTime()
		}
		endTime = frame.Timestamp.AsTime()

		// Process frame through event detector
		detector.DetectEvents(frame)

		// Collect any detected events synchronously
		select {
		case detectedEvents := <-detector.EventsChan():
			frame.Events = append(frame.Events, detectedEvents...)
		default:
			// No events detected
		}

		// Skip frames without events
		if len(frame.Events) == 0 {
			continue
		}

		eventCount += len(frame.Events)

		if verbose {
			for _, event := range frame.Events {
				logger.Info("Event detected",
					zap.String("type", getEventTypeName(event)),
					zap.Uint32("frame", frame.FrameIndex),
					zap.Time("timestamp", frame.Timestamp.AsTime()))
			}
		}

		// Send events to API (unless dry-run)
		if !dryRun && client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			resp, err := client.StoreSessionEvent(ctx, frame)
			cancel()

			if err != nil {
				logger.Warn("Failed to send event",
					zap.Error(err),
					zap.Int("event_count", len(frame.Events)))
			} else {
				eventsSent += len(frame.Events)
				if verbose {
					logger.Debug("Events sent successfully",
						zap.Bool("success", resp.Success),
						zap.Int("event_count", len(frame.Events)))
				}
			}
		} else if dryRun {
			eventsSent += len(frame.Events)
		}
	}

	// Print summary
	duration := endTime.Sub(startTime)
	logger.Info("Event processing complete",
		zap.Int("frames_processed", frameCount),
		zap.Int("events_detected", eventCount),
		zap.Int("events_sent", eventsSent),
		zap.Duration("recording_duration", duration),
		zap.String("start_time", startTime.Format("2006-01-02 15:04:05")),
		zap.String("end_time", endTime.Format("2006-01-02 15:04:05")))

	fmt.Printf("\n=== Send Events Summary ===\n")
	fmt.Printf("File: %s\n", filename)
	fmt.Printf("Frames processed: %d\n", frameCount)
	fmt.Printf("Events detected: %d\n", eventCount)
	if dryRun {
		fmt.Printf("Events sent: %d (dry-run)\n", eventsSent)
	} else {
		fmt.Printf("Events sent: %d\n", eventsSent)
	}
	fmt.Printf("Recording duration: %v\n", duration)

	return nil
}
