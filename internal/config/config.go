package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config holds all configuration for the application
type Config struct {
	// Global configuration
	Debug      bool   `yaml:"debug" mapstructure:"debug"`
	LogLevel   string `yaml:"log_level" mapstructure:"log_level"`
	LogFile    string `yaml:"log_file" mapstructure:"log_file"`
	ConfigFile string `yaml:"config" mapstructure:"config"`

	// Agent configuration
	Agent AgentConfig `yaml:"agent" mapstructure:"agent"`

	// API Server configuration
	APIServer APIServerConfig `yaml:"apiserver" mapstructure:"apiserver"`

	// Converter configuration
	Converter ConverterConfig `yaml:"converter" mapstructure:"converter"`

	// Replayer configuration
	Replayer ReplayerConfig `yaml:"replayer" mapstructure:"replayer"`
}

// AgentConfig holds configuration for the agent subcommand
type AgentConfig struct {
	Frequency       int    `yaml:"frequency" mapstructure:"frequency"`
	Format          string `yaml:"format" mapstructure:"format"`
	OutputDirectory string `yaml:"output_directory" mapstructure:"output_directory"`

	// JWT token for API authentication (used for both stream and events APIs)
	JWTToken string `yaml:"jwt_token" mapstructure:"jwt_token"`

	// Events API configuration
	EventsEnabled bool   `yaml:"events_enabled" mapstructure:"events_enabled"`
	EventsURL     string `yaml:"events_url" mapstructure:"events_url"`
}

// APIServerConfig holds configuration for the API server subcommand
type APIServerConfig struct {
	ServerAddress string `yaml:"server_address" mapstructure:"server_address"`
	MongoURI      string `yaml:"mongo_uri" mapstructure:"mongo_uri"`
	JWTSecret     string `yaml:"jwt_secret" mapstructure:"jwt_secret"`

	// Capture storage configuration
	CaptureDir       string `yaml:"capture_dir" mapstructure:"capture_dir"`
	CaptureRetention string `yaml:"capture_retention" mapstructure:"capture_retention"` // Duration string (e.g., "24h", "7d")
	CaptureMaxSize   int64  `yaml:"capture_max_size" mapstructure:"capture_max_size"`   // Max storage in bytes

	// Rate limiting
	MaxStreamHz int `yaml:"max_stream_hz" mapstructure:"max_stream_hz"` // Max frames per second from clients

	// Metrics
	MetricsAddr string `yaml:"metrics_addr" mapstructure:"metrics_addr"` // Prometheus metrics endpoint address
}

// ConverterConfig holds configuration for the converter subcommand
type ConverterConfig struct {
	InputFile  string `yaml:"input_file" mapstructure:"input_file"`
	OutputFile string `yaml:"output_file" mapstructure:"output_file"`
	OutputDir  string `yaml:"output_dir" mapstructure:"output_dir"`
	Format     string `yaml:"format" mapstructure:"format"`
	Verbose    bool   `yaml:"verbose" mapstructure:"verbose"`
	Overwrite  bool   `yaml:"overwrite" mapstructure:"overwrite"`
}

// ReplayerConfig holds configuration for the replayer subcommand
type ReplayerConfig struct {
	BindAddress string   `yaml:"bind_address" mapstructure:"bind_address"`
	Loop        bool     `yaml:"loop" mapstructure:"loop"`
	Files       []string `yaml:"files" mapstructure:"files"`
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
			EventsURL:       "http://localhost:8081",
		},
		APIServer: APIServerConfig{
			ServerAddress:    ":8081",
			MongoURI:         "mongodb://localhost:27017",
			JWTSecret:        "",
			CaptureDir:       "./captures",
			CaptureRetention: "168h",                  // 7 days
			CaptureMaxSize:   10 * 1024 * 1024 * 1024, // 10GB
			MaxStreamHz:      60,
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

// LoadConfig loads configuration from file, environment variables, and CLI flags
func LoadConfig(configFile string) (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	config := DefaultConfig()

	// Set up viper for config file and environment variables
	// Use a local viper instance to avoid conflicts with flag bindings
	v := viper.New()
	v.SetConfigType("yaml")

	// Load config file if specified
	if configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("error reading config file: %w", err)
			}
			// Config file not found; ignore error
		}
	}

	// Set up environment variable support
	v.SetEnvPrefix("NEVR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// Unmarshal config from file and environment variables
	if err := v.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return config, nil
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
	if c.APIServer.JWTSecret == "" {
		return fmt.Errorf("jwt secret must be specified")
	}
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
