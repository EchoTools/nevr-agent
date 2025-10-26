package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/echotools/evr-data-recorder/v3/internal/agent"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var version string = "v1.0.0"

type Flags struct {
	Targets         map[string][]int
	Frequency       int
	Format          string
	OutputDirectory string
	LogPath         string
	Debug           bool
	// Stream configuration
	StreamEnabled   bool
	StreamHTTPURL   string
	StreamSocketURL string
	StreamHTTPKey   string
	StreamServerKey string
	StreamUsername  string
	StreamPassword  string
}

var opts = Flags{}

func newLogger() *zap.Logger {
	var logger *zap.Logger
	level := zap.InfoLevel
	if opts.Debug {
		level = zap.DebugLevel
	}
	// Log to a file
	if opts.LogPath != "" {
		// Create a new logger that logs to a file
		cfg := zap.NewProductionConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
		cfg.OutputPaths = []string{opts.LogPath}
		cfg.ErrorOutputPaths = []string{opts.LogPath}

		cfg.Level.SetLevel(level)
		fileLogger, _ := cfg.Build()

		defer fileLogger.Sync() // flushes buffer, if any

		// Create a new logger that logs to the console
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}

		cfg.Level.SetLevel(level)

		consoleLogger, _ := cfg.Build()
		defer consoleLogger.Sync() // flushes buffer, if any

		// Create a new logger that logs to both the file and the console
		core := zapcore.NewTee(
			fileLogger.Core(),
			consoleLogger.Core(),
		)
		logger = zap.New(core)
	} else {
		cfg := zap.NewProductionConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
		cfg.Level.SetLevel(level)
		logger, _ = cfg.Build()
	}
	defer logger.Sync() // flushes buffer, if any
	return logger
}

func parseFlags() {
	flag.IntVar(&opts.Frequency, "frequency", 10, "Frequency in Hz")
	flag.BoolVar(&opts.Debug, "debug", false, "Enable debug logging")
	flag.StringVar(&opts.LogPath, "log", "", "Log file path")
	// Output options
	flag.StringVar(&opts.Format, "format", "replay", "Output format (replay, rtapi)")
	flag.StringVar(&opts.OutputDirectory, "output", "output", "Output directory")
	// Stream options
	flag.BoolVar(&opts.StreamEnabled, "stream", false, "Enable streaming to Nakama server")
	flag.StringVar(&opts.StreamHTTPURL, "stream-http", "https://g.echovrce.com:7350", "Stream HTTP URL")
	flag.StringVar(&opts.StreamSocketURL, "stream-socket", "wss://g.echovrce.com:7350/ws", "Stream WebSocket URL")
	flag.StringVar(&opts.StreamHTTPKey, "stream-http-key", "this_is_the_http_key", "Stream HTTP key")
	flag.StringVar(&opts.StreamServerKey, "stream-server-key", "this_is_the_server_key", "Stream server key")
	flag.StringVar(&opts.StreamUsername, "stream-username", "", "Stream username")
	flag.StringVar(&opts.StreamPassword, "stream-password", "", "Stream password")

	// Set usage
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] host:port[-endPort] [host:port[-endPort]...]\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Version: %s\n", version)
		flag.PrintDefaults()
		// include version

	}

	flag.Parse()

	// Parse N arguments as host:port or host:startPort-endPort
	if flag.NArg() != 1 {
		// Show help
		flag.Usage()
		// Exit
		os.Exit(1)
	}
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

func main() {

	parseFlags()
	logger := newLogger()

	opts.Targets = make(map[string][]int)
	for _, hostPort := range flag.Args() {
		host, ports, err := parseHostPort(hostPort)
		if err != nil {
			logger.Fatal("Failed to parse host:port", zap.String("host_port", hostPort), zap.Error(err))
		}
		opts.Targets[host] = ports
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	if opts.Frequency <= 0 {
		logger.Fatal("Frequency must be greater than 0", zap.Int("frequency", opts.Frequency))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go start(ctx, logger, opts)

	select {
	case <-ctx.Done():
		logger.Info("Context done, shutting down")
	case <-interrupt:
		logger.Info("Received interrupt signal, shutting down")
		cancel()
	}
	<-time.After(2 * time.Second) // Wait a bit to allow any ongoing operations to finish
	logger.Info("Exiting gracefully")
}

func start(ctx context.Context, logger *zap.Logger, opts Flags) {
	client := &http.Client{
		Timeout: 3 * time.Second, // Overall request timeout
		Transport: &http.Transport{
			MaxConnsPerHost:       2,
			DisableCompression:    true,
			MaxIdleConns:          2, // Set MaxIdleConns to 0 to close the connection after every request
			MaxIdleConnsPerHost:   2, // Set MaxIdleConnsPerHost to 0 to close the connection after every request
			IdleConnTimeout:       5 * time.Second,
			TLSHandshakeTimeout:   2 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 5 * time.Second,
			}).DialContext,
		},
	}
	// Create the output directory if it doesn't exist
	if err := os.MkdirAll(opts.OutputDirectory, 0755); err != nil {
		logger.Fatal("Failed to create output directory", zap.String("output_directory", opts.OutputDirectory), zap.Error(err))
	}
	// For each port in the target list, check if the port is open, then start polling
	sessions := make(map[string]agent.FrameWriter)

	interval := time.Second / time.Duration(opts.Frequency)
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

		logger.Debug("Scanning targets", zap.Any("targets", opts.Targets))
		for host, ports := range opts.Targets {
			logger := logger.With(zap.String("host", host))
			<-scanTicker.C // Add a small delay to avoid hammering the server

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
						logger.Warn("Failed to get session metadata", zap.Error(err))
					}
					continue
				}
				if meta.SessionUUID == "" {
					continue
				}

				logger.Debug("Retrieved session metadata", zap.Any("meta", meta))

				var filename string
				var outputPath string
				var fileWriter agent.FrameWriter

				// Create the appropriate file writer based on format
				formats := strings.Split(opts.Format, ",")

				if len(formats) > 1 {
					// Create multi-writer
					writers := make([]agent.FrameWriter, 0, len(formats))
					for _, format := range formats {
						format = strings.TrimSpace(format)
						var fw agent.FrameWriter
						switch format {
						case "stream":
							rtapiWriter := agent.NewStreamWriter(logger, opts.StreamHTTPURL, opts.StreamSocketURL, opts.StreamHTTPKey, opts.StreamServerKey, opts.StreamUsername, opts.StreamPassword)
							if err := rtapiWriter.Connect(); err != nil {
								logger.Error("Failed to connect stream writer", zap.Error(err))
								continue
							}
							logger.Info("Stream writer connected successfully")
							// Create multi-writer to write to both file and stream

							fw = rtapiWriter
						case "replay":
							fallthrough
						default:
							filename = agent.EchoReplaySessionFilename(time.Now(), meta.SessionUUID)
							outputPath = filepath.Join(opts.OutputDirectory, filename)
							replayWriter := agent.NewFrameDataLogSession(ctx, logger, outputPath, meta.SessionUUID)
							go replayWriter.ProcessFrames()
							fw = replayWriter
						}
						writers = append(writers, fw)
					}
					fileWriter = agent.NewMultiWriter(logger, writers...)
				} else {
					switch formats[0] {
					case "stream":
						rtapiWriter := agent.NewStreamWriter(logger, opts.StreamHTTPURL, opts.StreamSocketURL, opts.StreamHTTPKey, opts.StreamServerKey, opts.StreamUsername, opts.StreamPassword)
						if err := rtapiWriter.Connect(); err != nil {
							logger.Error("Failed to connect stream writer", zap.Error(err))
							continue
						}
						logger.Info("Stream writer connected successfully")
						// Create multi-writer to write to both file and stream
						fileWriter = rtapiWriter
					case "replay":
						fallthrough
					default:
						filename = agent.EchoReplaySessionFilename(time.Now(), meta.SessionUUID)
						outputPath = filepath.Join(opts.OutputDirectory, filename)
						replayWriter := agent.NewFrameDataLogSession(ctx, logger, outputPath, meta.SessionUUID)
						go replayWriter.ProcessFrames()
						fileWriter = replayWriter
					}
				}
				logger = logger.With(zap.String("session_uuid", meta.SessionUUID), zap.String("filename", filename))

				var session agent.FrameWriter = fileWriter

				// If streaming is enabled, create stream writer and multi-writer
				if opts.StreamEnabled {
					streamWriter := agent.NewStreamWriter(logger, opts.StreamHTTPURL, opts.StreamSocketURL, opts.StreamHTTPKey, opts.StreamServerKey, opts.StreamUsername, opts.StreamPassword)
					if err := streamWriter.Connect(); err != nil {
						logger.Error("Failed to connect stream writer", zap.Error(err))
					} else {
						logger.Info("Stream writer connected successfully")
						// Create multi-writer to write to both file and stream
						session = agent.NewMultiWriter(logger, fileWriter, streamWriter)
					}
				}

				sessions[baseURL] = session
				go agent.NewHTTPFramePoller(session.Context(), logger, client, baseURL, interval, session)

				logger.Info("Added new frame client",
					zap.String("file_path", outputPath),
					zap.Bool("streaming_enabled", opts.StreamEnabled))
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

func parsePortRange(port string) ([]int, error) {

	// 1234,3456,7890-10111
	portRanges := strings.Split(port, ",")

	ports := make([]int, 0)

	for _, rangeStr := range portRanges {
		rangeStr = strings.TrimSpace(rangeStr)
		if rangeStr == "" {
			continue
		}
		parts := strings.SplitN(rangeStr, "-", 2)
		if len(parts) > 2 {
			return nil, fmt.Errorf("invalid port range `%s`", rangeStr)
		}

		if len(parts) == 1 {
			port, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid port `%s`: %v", rangeStr, err)
			}
			ports = append(ports, port)
		} else {
			startPort, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid port `%s`: %v", port, err)
			}
			endPort, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid port `%s`: %v", port, err)
			}
			if startPort > endPort {
				return nil, fmt.Errorf("invalid port range `%s`: startPort must be less than or equal to endPort", rangeStr)
			}

			for i := startPort; i <= endPort; i++ {
				ports = append(ports, i)
			}
		}

		for _, port := range ports {
			if port < 0 || port > 65535 {
				return nil, fmt.Errorf("invalid port `%d`: port must be between 0 and 65535", port)
			}
		}
	}
	return ports, nil
}
