package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/echotools/nevr-agent/v4/internal/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// zapLoggerAdapter adapts zap.Logger to api.Logger interface
type zapLoggerAdapter struct {
	logger *zap.Logger
}

func (z *zapLoggerAdapter) Debug(msg string, fields ...any) {
	z.logger.Sugar().Debugw(msg, fields...)
}

func (z *zapLoggerAdapter) Info(msg string, fields ...any) {
	z.logger.Sugar().Infow(msg, fields...)
}

func (z *zapLoggerAdapter) Error(msg string, fields ...any) {
	z.logger.Sugar().Errorw(msg, fields...)
}

func (z *zapLoggerAdapter) Warn(msg string, fields ...any) {
	z.logger.Sugar().Warnw(msg, fields...)
}

func newAPIServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the telemetry API server",
		Long: `The serve command starts an HTTP server that provides endpoints 
for storing and retrieving telemetry data, with optional capture storage
and real-time streaming support.`,
		Example: `  # Start API server on default port
	agent serve

  # Start with custom MongoDB URI
	agent serve --mongo-uri mongodb://localhost:27017

  # Enable capture storage with retention
	agent serve --capture-dir ./captures --capture-retention 168h

  # Enable Prometheus metrics
	agent serve --metrics-addr :9090

  # Use a config file
	agent serve -c config.yaml`,
		RunE: runAPIServer,
	}

	// APIServer-specific flags
	cmd.Flags().String("server-address", ":8081", "Server listen address")
	cmd.Flags().String("mongo-uri", "mongodb://localhost:27017", "MongoDB connection URI")
	cmd.Flags().String("jwt-secret", "", "JWT secret key for token validation")

	// Capture storage flags
	cmd.Flags().String("capture-dir", "./captures", "Directory to store nevrcap capture files")
	cmd.Flags().String("capture-retention", "168h", "How long to keep capture files (e.g., 24h, 7d)")
	cmd.Flags().Int64("capture-max-size", 10*1024*1024*1024, "Maximum storage for captures in bytes")

	// Rate limiting
	cmd.Flags().Int("max-stream-hz", 60, "Maximum frames per second to accept from clients")

	// Metrics
	cmd.Flags().String("metrics-addr", "", "Prometheus metrics endpoint address (e.g., :9090)")

	// Bind flags to viper
	viper.BindPFlags(cmd.Flags())

	return cmd
}

func runAPIServer(cmd *cobra.Command, args []string) error {
	// Override config with command flags
	cfg.APIServer.ServerAddress = viper.GetString("server-address")
	cfg.APIServer.MongoURI = viper.GetString("mongo-uri")
	cfg.APIServer.JWTSecret = viper.GetString("jwt-secret")
	cfg.APIServer.CaptureDir = viper.GetString("capture-dir")
	cfg.APIServer.CaptureRetention = viper.GetString("capture-retention")
	cfg.APIServer.CaptureMaxSize = viper.GetInt64("capture-max-size")
	cfg.APIServer.MaxStreamHz = viper.GetInt("max-stream-hz")
	cfg.APIServer.MetricsAddr = viper.GetString("metrics-addr")

	// Validate configuration
	if err := cfg.ValidateAPIServerConfig(); err != nil {
		return err
	}

	logger.Info("Starting API server",
		zap.String("server_address", cfg.APIServer.ServerAddress),
		zap.String("mongo_uri", cfg.APIServer.MongoURI),
		zap.String("capture_dir", cfg.APIServer.CaptureDir),
		zap.String("capture_retention", cfg.APIServer.CaptureRetention),
		zap.Int64("capture_max_size", cfg.APIServer.CaptureMaxSize),
		zap.Int("max_stream_hz", cfg.APIServer.MaxStreamHz),
		zap.String("metrics_addr", cfg.APIServer.MetricsAddr))

	// Create service configuration
	serviceConfig := api.DefaultConfig()
	serviceConfig.MongoURI = cfg.APIServer.MongoURI
	serviceConfig.ServerAddress = cfg.APIServer.ServerAddress
	serviceConfig.JWTSecret = cfg.APIServer.JWTSecret
	serviceConfig.CaptureDir = cfg.APIServer.CaptureDir
	serviceConfig.CaptureRetention = cfg.APIServer.CaptureRetention
	serviceConfig.CaptureMaxSize = cfg.APIServer.CaptureMaxSize
	serviceConfig.MaxStreamHz = cfg.APIServer.MaxStreamHz
	serviceConfig.MetricsAddr = cfg.APIServer.MetricsAddr

	// Create service
	service, err := api.NewService(serviceConfig, &zapLoggerAdapter{logger: logger})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Initialize service
	ctx := context.Background()
	if err := service.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize service: %w", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutdown signal received, stopping service...")
		cancel()
	}()

	// Start service
	logger.Info("Starting session events service",
		zap.String("address", cfg.APIServer.ServerAddress))
	logger.Info("Available endpoints:",
		zap.String("POST", "/lobby-session-events - Store session event"),
		zap.String("GET", "/lobby-session-events/{match_id} - Get session events by match ID"),
		zap.String("WebSocket", "/v3/stream - WebSocket stream with JWT auth"),
		zap.String("GET", "/health - Health check"))

	if err := service.Start(ctx); err != nil {
		logger.Info("Service stopped", zap.Error(err))
	}

	// Stop service
	if err := service.Stop(context.Background()); err != nil {
		logger.Warn("Error stopping service", zap.Error(err))
	}

	logger.Info("API server stopped gracefully")
	return nil
}
