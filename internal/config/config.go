package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the application
type Config struct {
	// Global configuration
	Debug      bool   `yaml:"debug"`
	LogLevel   string `yaml:"log_level"`
	LogFile    string `yaml:"log_file"`
	ConfigFile string `yaml:"-"` // Not loaded from yaml

	// Agent configuration
	Agent AgentConfig `yaml:"agent"`

	// API Server configuration
	APIServer APIServerConfig `yaml:"apiserver"`

	// Converter configuration
	Converter ConverterConfig `yaml:"converter"`

	// Replayer configuration
	Replayer ReplayerConfig `yaml:"replayer"`
}

// AgentConfig holds configuration for the agent subcommand
type AgentConfig struct {
	Frequency       int    `yaml:"frequency"`
	Format          string `yaml:"format"`
	OutputDirectory string `yaml:"output_directory"`

	// JWT token for API authentication (used for stream APIs)
	JWTToken string `yaml:"jwt_token"`
}

// APIServerConfig holds configuration for the API server subcommand
type APIServerConfig struct {
	ServerAddress string `yaml:"server_address"`
	MongoURI      string `yaml:"mongo_uri"`
	JWTSecret     string `yaml:"jwt_secret"`

	// AMQP configuration
	AMQPEnabled   bool   `yaml:"amqp_enabled"`
	AMQPURI       string `yaml:"amqp_uri"`
	AMQPQueueName string `yaml:"amqp_queue_name"`

	// Capture storage configuration
	CaptureDir       string `yaml:"capture_dir"`
	CaptureRetention string `yaml:"capture_retention"` // Duration string (e.g., "24h", "7d")
	CaptureMaxSize   int64  `yaml:"capture_max_size"`  // Max storage in bytes

	// Rate limiting
	MaxStreamHz int `yaml:"max_stream_hz"` // Max frames per second from clients

	// CORS configuration
	CORSOrigins string `yaml:"cors_origins"` // Comma-separated list of allowed origins

	// Metrics
	MetricsAddr string `yaml:"metrics_addr"` // Prometheus metrics endpoint address
}

// ConverterConfig holds configuration for the converter subcommand
type ConverterConfig struct {
	InputFile  string `yaml:"input_file"`
	OutputFile string `yaml:"output_file"`
	OutputDir  string `yaml:"output_dir"`
	Format     string `yaml:"format"`
	Verbose    bool   `yaml:"verbose"`
	Overwrite  bool   `yaml:"overwrite"`
}

// ReplayerConfig holds configuration for the replayer subcommand
type ReplayerConfig struct {
	BindAddress string   `yaml:"bind_address"`
	Loop        bool     `yaml:"loop"`
	Files       []string `yaml:"files"`
}

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	return &Config{
		Debug:    false,
		LogLevel: "info",
		LogFile:  "",
		Agent: AgentConfig{
			Frequency:       10,
			Format:          "nevrcap",
			OutputDirectory: "output",
		},
		APIServer: APIServerConfig{
			ServerAddress:    ":8081",
			MongoURI:         "mongodb://localhost:27017",
			JWTSecret:        "",
			AMQPEnabled:      false,
			AMQPURI:          "amqp://guest:guest@localhost:5672/",
			AMQPQueueName:    "match.events",
			CaptureDir:       "./captures",
			CaptureRetention: "168h",                  // 7 days
			CaptureMaxSize:   10 * 1024 * 1024 * 1024, // 10GB
			MaxStreamHz:      60,
			CORSOrigins:      "*",
			MetricsAddr:      "",
		},
		Converter: ConverterConfig{
			OutputDir: "./",
			Format:    "auto",
		},
		Replayer: ReplayerConfig{
			BindAddress: "127.0.0.1:6721",
			Loop:        false,
		},
	}
}

// LoadConfig loads configuration from file and environment variables.
// Priority: defaults < config file < environment variables
// CLI flags are handled separately by the command layer.
func LoadConfig(configFile string) (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Start with defaults
	config := DefaultConfig()
	config.ConfigFile = configFile

	// Load config file if specified
	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("error parsing config file: %w", err)
		}
	}

	// Override with environment variables
	applyEnvOverrides(config)

	return config, nil
}

// applyEnvOverrides applies environment variable overrides to config.
// Supports both NEVR_ and EVR_ prefixes for backwards compatibility.
func applyEnvOverrides(c *Config) {
	// Helper to get env with fallback prefix
	getEnv := func(key string) string {
		if v := os.Getenv("NEVR_" + key); v != "" {
			return v
		}
		return os.Getenv("EVR_" + key)
	}

	// Global
	if v := getEnv("DEBUG"); v != "" {
		c.Debug = v == "true" || v == "1"
	}
	if v := getEnv("LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := getEnv("LOG_FILE"); v != "" {
		c.LogFile = v
	}

	// Agent
	if v := getEnv("AGENT_JWT_TOKEN"); v != "" {
		c.Agent.JWTToken = v
	}

	// API Server
	if v := getEnv("APISERVER_SERVER_ADDRESS"); v != "" {
		c.APIServer.ServerAddress = v
	}
	if v := getEnv("APISERVER_MONGO_URI"); v != "" {
		c.APIServer.MongoURI = v
	}
	if v := getEnv("APISERVER_JWT_SECRET"); v != "" {
		c.APIServer.JWTSecret = v
	}
	if v := getEnv("APISERVER_CAPTURE_DIR"); v != "" {
		c.APIServer.CaptureDir = v
	}
	if v := getEnv("APISERVER_CAPTURE_RETENTION"); v != "" {
		c.APIServer.CaptureRetention = v
	}
	if v := getEnv("APISERVER_METRICS_ADDR"); v != "" {
		c.APIServer.MetricsAddr = v
	}
	if v := getEnv("APISERVER_MAX_STREAM_HZ"); v != "" {
		if hz, err := strconv.Atoi(v); err == nil {
			c.APIServer.MaxStreamHz = hz
		}
	}
	if v := getEnv("APISERVER_CORS_ORIGINS"); v != "" {
		c.APIServer.CORSOrigins = v
	}
	// AMQP configuration
	if v := getEnv("APISERVER_AMQP_ENABLED"); v != "" {
		c.APIServer.AMQPEnabled = v == "true" || v == "1"
	}
	if v := getEnv("APISERVER_AMQP_URI"); v != "" {
		c.APIServer.AMQPURI = v
	}
	if v := getEnv("APISERVER_AMQP_QUEUE_NAME"); v != "" {
		c.APIServer.AMQPQueueName = v
	}
}

// NewLogger creates a zap logger based on the configuration
func (c *Config) NewLogger() (*zap.Logger, error) {
	var level zapcore.Level
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		if c.Debug {
			level = zapcore.DebugLevel
		} else {
			level = zapcore.InfoLevel
		}
	}

	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.Level.SetLevel(level)

	// Include caller info in log messages (relative path and line number)
	cfg.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	if c.LogFile != "" {
		// Log to file and console
		cfg.OutputPaths = []string{c.LogFile, "stdout"}
		cfg.ErrorOutputPaths = []string{c.LogFile, "stderr"}
	} else {
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}
	}

	logger, err := cfg.Build(zap.AddCaller())
	if err != nil {
		return nil, fmt.Errorf("error creating logger: %w", err)
	}

	return logger, nil
}

// ValidateAgentConfig validates agent-specific configuration
func (c *Config) ValidateAgentConfig() error {
	if c.Agent.Frequency <= 0 {
		return fmt.Errorf("frequency must be greater than 0")
	}

	// Check if we need to validate output directory
	needsOutput := false
	formats := strings.Split(c.Agent.Format, ",")
	for _, f := range formats {
		f = strings.TrimSpace(f)
		if f != "" && f != "none" {
			needsOutput = true
			break
		}
	}

	if needsOutput {
		if c.Agent.OutputDirectory == "" {
			return fmt.Errorf("output directory must be specified for format %s", c.Agent.Format)
		}
		if err := os.MkdirAll(c.Agent.OutputDirectory, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}
	return nil
}

// ValidateAPIServerConfig validates API server configuration
func (c *Config) ValidateAPIServerConfig() error {
	if c.APIServer.ServerAddress == "" {
		return fmt.Errorf("server address must be specified")
	}
	if c.APIServer.MongoURI == "" {
		return fmt.Errorf("mongo URI must be specified")
	}
	// JWT secret is optional - if not set, authentication is disabled
	return nil
}

// ValidateConverterConfig validates converter configuration
func (c *Config) ValidateConverterConfig() error {
	if c.Converter.InputFile == "" {
		return fmt.Errorf("input file must be specified")
	}
	if _, err := os.Stat(c.Converter.InputFile); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", c.Converter.InputFile)
	}
	return nil
}

// ValidateReplayerConfig validates replayer configuration
func (c *Config) ValidateReplayerConfig() error {
	if c.Replayer.BindAddress == "" {
		return fmt.Errorf("bind address must be specified")
	}
	if len(c.Replayer.Files) == 0 {
		return fmt.Errorf("at least one replay file must be specified")
	}
	for _, file := range c.Replayer.Files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return fmt.Errorf("replay file does not exist: %s", file)
		}
	}
	return nil
}
