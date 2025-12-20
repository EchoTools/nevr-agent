package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/agent"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// StreamConfig holds configuration for the stream command
type StreamConfig struct {
	Frequency     int
	Format        string
	OutputDir     string
	Events        bool
	EventsStream  bool
	EventsURL     string
	AllFrames     bool     // Send all frames, not just event frames
	FPS           int      // Target frames per second for streaming
	IncludeModes  []string // Only stream these game modes
	ExcludeModes  []string // Exclude these game modes from streaming
	ExcludeBones  bool     // Exclude player bone data
	ActiveOnly    bool     // Only stream frames during active gameplay
	ExcludePaused bool     // Exclude paused frames (only with ActiveOnly)
	IdleFPS       int      // Frame rate for non-gametime frames
}

func newAgentCommand() *cobra.Command {
	var (
		frequency     int
		format        string
		outputDir     string
		events        bool
		eventsStream  bool
		eventsURL     string
		allFrames     bool
		fps           int
		includeModes  []string
		excludeModes  []string
		excludeBones  bool
		activeOnly    bool
		excludePaused bool
		idleFPS       int
	)

	cmd := &cobra.Command{
		Use:   "stream [flags] <host:port[-endPort]> [host:port[-endPort]...]",
		Short: "Record session and player bone data from EchoVR game servers",
		Long: `The stream command regularly scans specified ports and starts polling 
the HTTP API at the configured frequency, storing output to files.

Targets are specified as host:port or host:startPort-endPort for port ranges.`,
		Example: `  # Record from ports 6721-6730 on localhost at 30Hz
  agent stream --frequency 30 --output ./output 127.0.0.1:6721-6730

  # Stream to events API without saving files locally
  agent stream --format none --events-stream --events-url http://localhost:8081 127.0.0.1:6721

  # Use a config file
  agent stream -c config.yaml 127.0.0.1:6721

  # Stream all frames at 30 FPS, excluding bone data
  agent stream --all-frames --fps 30 --exclude-bones 127.0.0.1:6721

  # Only stream Echo Arena matches during active gameplay
  agent stream --include-modes echo_arena --active-only 127.0.0.1:6721`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			streamCfg := StreamConfig{
				Frequency:     frequency,
				Format:        format,
				OutputDir:     outputDir,
				Events:        events,
				EventsStream:  eventsStream,
				EventsURL:     eventsURL,
				AllFrames:     allFrames,
				FPS:           fps,
				IncludeModes:  includeModes,
				ExcludeModes:  excludeModes,
				ExcludeBones:  excludeBones,
				ActiveOnly:    activeOnly,
				ExcludePaused: excludePaused,
				IdleFPS:       idleFPS,
			}
			return runAgent(cmd, args, streamCfg)
		},
	}

	// Agent-specific flags
	cmd.Flags().IntVarP(&frequency, "frequency", "f", 10, "Polling frequency in Hz")
	cmd.Flags().StringVar(&format, "format", "replay", "Output format (replay, none, or comma-separated)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "output", "Output directory for recorded files")

	// Events API options
	cmd.Flags().BoolVar(&events, "events", false, "Enable sending frames to events API")
	cmd.Flags().BoolVar(&eventsStream, "events-stream", false, "Enable streaming frames to events API via WebSocket")
	cmd.Flags().StringVar(&eventsURL, "events-url", "http://localhost:8081", "Base URL of the events API")

	// Stream filtering options
	cmd.Flags().BoolVar(&allFrames, "all-frames", false, "Send all frames, not just frames with events")
	cmd.Flags().IntVar(&fps, "fps", 0, "Target frames per second for streaming (0 = use polling frequency)")
	cmd.Flags().StringSliceVar(&includeModes, "include-modes", nil, "Only stream these game modes (e.g., echo_arena,echo_arena_private)")
	cmd.Flags().StringSliceVar(&excludeModes, "exclude-modes", nil, "Exclude these game modes from streaming")
	cmd.Flags().BoolVar(&excludeBones, "exclude-bones", false, "Exclude player bone data from frames")
	cmd.Flags().BoolVar(&activeOnly, "active-only", false, "Only stream frames during active gameplay (game_status=playing)")
	cmd.Flags().BoolVar(&excludePaused, "exclude-paused", false, "Exclude paused frames (only effective with --active-only)")
	cmd.Flags().IntVar(&idleFPS, "idle-fps", 1, "Frame rate for non-gametime frames (lobby, paused, etc.)")

	return cmd
}

func runAgent(cmd *cobra.Command, args []string, streamCfg StreamConfig) error {
	// Override config with command flags
	cfg.Agent.Frequency = streamCfg.Frequency
	cfg.Agent.Format = streamCfg.Format
	cfg.Agent.OutputDirectory = streamCfg.OutputDir
	cfg.Agent.EventsEnabled = streamCfg.Events
	cfg.Agent.EventsURL = streamCfg.EventsURL

	// If only streaming to events API, we don't need file output
	if streamCfg.EventsStream || streamCfg.Events {
		// Check if any file format is specified
		hasFileFormat := false
		for _, f := range strings.Split(streamCfg.Format, ",") {
			f = strings.TrimSpace(f)
			if f != "" && f != "none" {
				hasFileFormat = true
				break
			}
		}
		if !hasFileFormat {
			// Override format to "none" to skip file output validation
			cfg.Agent.Format = "none"
		}
	}

	targets := make(map[string][]int)
	for _, hostPort := range args {
		host, ports, err := parseHostPort(hostPort)
		if err != nil {
			return fmt.Errorf("failed to parse host:port %q: %w", hostPort, err)
		}
		targets[host] = ports
	}

	// Validate configuration
	if err := cfg.ValidateAgentConfig(); err != nil {
		return err
	}

	logger.Info("Starting agent",
		zap.Int("frequency", cfg.Agent.Frequency),
		zap.String("format", cfg.Agent.Format),
		zap.String("output_directory", cfg.Agent.OutputDirectory),
		zap.Bool("all_frames", streamCfg.AllFrames),
		zap.Int("fps", streamCfg.FPS),
		zap.Strings("include_modes", streamCfg.IncludeModes),
		zap.Strings("exclude_modes", streamCfg.ExcludeModes),
		zap.Bool("exclude_bones", streamCfg.ExcludeBones),
		zap.Bool("active_only", streamCfg.ActiveOnly),
		zap.Bool("exclude_paused", streamCfg.ExcludePaused),
		zap.Int("idle_fps", streamCfg.IdleFPS),
		zap.Any("targets", targets))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go startAgent(ctx, logger, targets, streamCfg)

	select {
	case <-ctx.Done():
		logger.Info("Context done, shutting down")
	case <-interrupt:
		logger.Info("Received interrupt signal, shutting down")
		cancel()
	}

	time.Sleep(2 * time.Second) // Allow ongoing operations to finish
	logger.Info("Agent stopped gracefully")
	return nil
}

func startAgent(ctx context.Context, logger *zap.Logger, targets map[string][]int, streamCfg StreamConfig) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			MaxConnsPerHost:       2,
			DisableCompression:    true,
			MaxIdleConns:          2,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       5 * time.Second,
			TLSHandshakeTimeout:   2 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 5 * time.Second,
			}).DialContext,
		},
	}

	sessions := make(map[string]agent.FrameWriter)
	interval := time.Second / time.Duration(cfg.Agent.Frequency)
	cycleTicker := time.NewTicker(100 * time.Millisecond)
	scanTicker := time.NewTicker(10 * time.Millisecond)

OuterLoop:
	for {
		select {
		case <-ctx.Done():
			return
		case <-cycleTicker.C:
			cycleTicker.Reset(5 * time.Second)
		}

		logger.Debug("Scanning targets", zap.Any("targets", targets))
		for host, ports := range targets {
			logger := logger.With(zap.String("host", host))
			<-scanTicker.C

			for _, port := range ports {
				select {
				case <-ctx.Done():
					break OuterLoop
				default:
				}

				logger := logger.With(zap.Int("port", port))
				baseURL := fmt.Sprintf("http://%s:%d", host, port)

				if s, found := sessions[baseURL]; found {
					if !s.IsStopped() {
						logger.Debug("session still active, skipping")
						continue
					} else {
						delete(sessions, baseURL)
					}
				}

				meta, err := agent.GetSessionMeta(baseURL)
				if err != nil {
					switch err {
					case agent.ErrAPIAccessDisabled:
						logger.Warn("API access is disabled on the server")
					default:
						logger.Debug("Failed to get session metadata", zap.Error(err))
					}
					continue
				}
				if meta.SessionUUID == "" {
					continue
				}

				logger.Debug("Retrieved session metadata", zap.Any("meta", meta))

				var filename string
				var outputPath string

				writers := make([]agent.FrameWriter, 0)

				// Create the appropriate file writer based on format
				formats := strings.Split(cfg.Agent.Format, ",")

				for _, format := range formats {
					format = strings.TrimSpace(format)
					if format == "" || format == "none" {
						continue
					}

					switch format {
					case "replay":
						fallthrough
					default:
						filename = agent.EchoReplaySessionFilename(time.Now(), meta.SessionUUID)
						outputPath = filepath.Join(cfg.Agent.OutputDirectory, filename)
						replayWriter := agent.NewFrameDataLogSession(ctx, logger, outputPath, meta.SessionUUID)
						go replayWriter.ProcessFrames()
						writers = append(writers, replayWriter)
					}
				}

				logger = logger.With(zap.String("session_uuid", meta.SessionUUID))
				if filename != "" {
					logger = logger.With(zap.String("filename", filename))
				}

				// If events sending is enabled, add EventsAPI writer
				if cfg.Agent.EventsEnabled {
					eventsWriter := agent.NewEventsAPIWriter(logger, streamCfg.EventsURL, cfg.Agent.JWTToken)
					writers = append(writers, eventsWriter)
				}
				// If events streaming is enabled, add WebSocket writer
				if streamCfg.EventsStream {
					// Derive WebSocket URL from Events URL if not explicitly set
					wsURL := streamCfg.EventsURL
					if strings.HasPrefix(wsURL, "http") {
						wsURL = strings.Replace(wsURL, "http", "ws", 1)
					}
					wsURL = strings.TrimSuffix(wsURL, "/") + "/v3/stream"

					wsWriter := agent.NewWebSocketWriter(logger, wsURL, cfg.Agent.JWTToken)
					if err := wsWriter.Connect(); err != nil {
						logger.Error("Failed to connect WebSocket writer", zap.Error(err))
					} else {
						logger.Info("WebSocket writer connected successfully", zap.String("url", wsURL))
						writers = append(writers, wsWriter)
					}
				}

				if len(writers) == 0 {
					logger.Warn("No output format or destination specified, skipping session")
					continue
				}

				var session agent.FrameWriter
				if len(writers) == 1 {
					session = writers[0]
				} else {
					session = agent.NewMultiWriter(logger, writers...)
				}

				sessions[baseURL] = session
				pollerCfg := agent.PollerConfig{
					AllFrames:     streamCfg.AllFrames,
					FPS:           streamCfg.FPS,
					IncludeModes:  streamCfg.IncludeModes,
					ExcludeModes:  streamCfg.ExcludeModes,
					ExcludeBones:  streamCfg.ExcludeBones,
					ActiveOnly:    streamCfg.ActiveOnly,
					ExcludePaused: streamCfg.ExcludePaused,
					IdleFPS:       streamCfg.IdleFPS,
				}
				go agent.NewHTTPFramePoller(session.Context(), logger, client, baseURL, interval, session, pollerCfg)

				logger.Info("Added new frame client",
					zap.String("file_path", outputPath))
			}
		}

		select {
		case <-ctx.Done():
			break OuterLoop
		case <-time.After(3 * time.Second):
		}
	}

	logger.Info("Finished processing all targets, exiting")
	for _, session := range sessions {
		session.Close()
	}
	logger.Info("Closed sessions")
}

func parseHostPort(s string) (string, []int, error) {
	components := strings.Split(s, ":")
	if len(components) != 2 {
		return "", nil, errors.New("invalid format, expected host:port or host:startPort-endPort")
	}

	host := components[0]
	ports, err := parsePortRange(components[1])
	if err != nil {
		return "", nil, err
	}

	return host, ports, nil
}

func parsePortRange(port string) ([]int, error) {
	portRanges := strings.Split(port, ",")
	ports := make([]int, 0)

	for _, rangeStr := range portRanges {
		rangeStr = strings.TrimSpace(rangeStr)
		if rangeStr == "" {
			continue
		}
		parts := strings.SplitN(rangeStr, "-", 2)
		if len(parts) > 2 {
			return nil, fmt.Errorf("invalid port range %q", rangeStr)
		}

		if len(parts) == 1 {
			port, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %v", rangeStr, err)
			}
			ports = append(ports, port)
		} else {
			startPort, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %v", port, err)
			}
			endPort, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %v", port, err)
			}
			if startPort > endPort {
				return nil, fmt.Errorf("invalid port range %q: startPort must be less than or equal to endPort", rangeStr)
			}

			for i := startPort; i <= endPort; i++ {
				ports = append(ports, i)
			}
		}

		for _, port := range ports {
			if port < 0 || port > 65535 {
				return nil, fmt.Errorf("invalid port %d: port must be between 0 and 65535", port)
			}
		}
	}
	return ports, nil
}
