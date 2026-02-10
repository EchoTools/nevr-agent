package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// Node identifier for this agent instance
	NodeID string `yaml:"node_id"`
}

// ConverterConfig holds configuration for the converter subcommand
type ConverterConfig struct {
	InputFile    string `yaml:"input_file"`
	OutputFile   string `yaml:"output_file"`
	OutputDir    string `yaml:"output_dir"`
	Format       string `yaml:"format"`
	Verbose      bool   `yaml:"verbose"`
	Overwrite    bool   `yaml:"overwrite"`
	ExcludeBones bool   `yaml:"exclude_bones"`
	Recursive    bool   `yaml:"recursive"`
	Glob         string `yaml:"glob"`
	Validate     bool   `yaml:"validate"`
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
			NodeID:           "", // Will use hostname if empty
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
	if v := getEnv("APISERVER_NODE_ID"); v != "" {
		c.APIServer.NodeID = v
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
	
	// Converter configuration
	if v := getEnv("CONVERTER_INPUT_FILE"); v != "" {
		c.Converter.InputFile = v
	}
	if v := getEnv("CONVERTER_OUTPUT_FILE"); v != "" {
		c.Converter.OutputFile = v
	}
	if v := getEnv("CONVERTER_OUTPUT_DIR"); v != "" {
		c.Converter.OutputDir = v
	}
	if v := getEnv("CONVERTER_FORMAT"); v != "" {
		c.Converter.Format = v
	}
	if v := getEnv("CONVERTER_VERBOSE"); v != "" {
		c.Converter.Verbose = parseBool(v)
	}
	if v := getEnv("CONVERTER_OVERWRITE"); v != "" {
		c.Converter.Overwrite = parseBool(v)
	}
	if v := getEnv("CONVERTER_EXCLUDE_BONES"); v != "" {
		c.Converter.ExcludeBones = parseBool(v)
	}
	if v := getEnv("CONVERTER_RECURSIVE"); v != "" {
		c.Converter.Recursive = parseBool(v)
	}
	if v := getEnv("CONVERTER_GLOB"); v != "" {
		c.Converter.Glob = v
	}
	if v := getEnv("CONVERTER_VALIDATE"); v != "" {
		c.Converter.Validate = parseBool(v)
	}
}

// parseBool parses a boolean value from a string
// Accepts: "true", "1", "yes", "on" (case-insensitive) as true
// Everything else is false
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
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
	cfg := &c.Converter
	
	// 1. Required fields validation
	if err := validateRequiredFields(cfg); err != nil {
		return err
	}
	
	// 2. Format validation
	if err := validateFormat(cfg); err != nil {
		return err
	}
	
	// 3. Flag combination rules
	if err := validateFlagCombinations(cfg); err != nil {
		return err
	}
	
	// 4. File system validation
	if err := validateFileSystem(cfg); err != nil {
		return err
	}
	
	// 5. Glob pattern validation
	if err := validateGlobPattern(cfg); err != nil {
		return err
	}
	
	return nil
}

// validateRequiredFields validates required converter configuration fields
func validateRequiredFields(cfg *ConverterConfig) error {
	// InputFile is required
	if cfg.InputFile == "" || strings.TrimSpace(cfg.InputFile) == "" {
		return fmt.Errorf("input file is required")
	}
	
	// Either OutputFile or OutputDir is required
	if cfg.OutputFile == "" && cfg.OutputDir == "" {
		return fmt.Errorf("either output file or output directory is required")
	}
	
	// Both OutputFile and OutputDir cannot be specified (ambiguous)
	if cfg.OutputFile != "" && cfg.OutputDir != "" {
		return fmt.Errorf("cannot specify both output file and output directory")
	}
	
	return nil
}

// validateFormat validates and normalizes the format field
func validateFormat(cfg *ConverterConfig) error {
	// Normalize format to lowercase and trim spaces
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	
	// Default empty format to "auto"
	if cfg.Format == "" {
		cfg.Format = "auto"
	}
	
	// Validate against allowed formats
	allowedFormats := map[string]bool{
		"auto":       true,
		"echoreplay": true,
		"nevrcap":    true,
	}
	
	if !allowedFormats[cfg.Format] {
		return fmt.Errorf("invalid format: %s (must be auto, echoreplay, or nevrcap)", cfg.Format)
	}
	
	return nil
}

// validateFlagCombinations validates flag combination rules
func validateFlagCombinations(cfg *ConverterConfig) error {
	// Validate flag cannot be used with Recursive or Glob
	if cfg.Validate && cfg.Recursive {
		return fmt.Errorf("--validate flag cannot be used with --recursive")
	}
	if cfg.Validate && cfg.Glob != "" {
		return fmt.Errorf("--validate flag cannot be used with --glob")
	}
	
	// Validate with ExcludeBones causes validation failures
	if cfg.Validate && cfg.ExcludeBones {
		return fmt.Errorf("--validate cannot be used with --exclude-bones (would cause validation to fail)")
	}
	
	// Recursive or Glob requires OutputDir
	if cfg.Recursive && cfg.OutputDir == "" {
		return fmt.Errorf("--output-dir is required when using --recursive")
	}
	if cfg.Glob != "" && cfg.OutputDir == "" {
		return fmt.Errorf("--output-dir is required when using --glob")
	}
	
	// Recursive or Glob with OutputFile is invalid (batch needs dir)
	if cfg.Recursive && cfg.OutputFile != "" {
		return fmt.Errorf("--output cannot be used with --recursive (output files will be auto-generated)")
	}
	if cfg.Glob != "" && cfg.OutputFile != "" {
		return fmt.Errorf("--output cannot be used with --glob (output files will be auto-generated)")
	}
	
	return nil
}

// validateFileSystem validates file system paths and permissions
func validateFileSystem(cfg *ConverterConfig) error {
	// Validate InputFile exists
	inputInfo, err := os.Stat(cfg.InputFile)
	if os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", cfg.InputFile)
	}
	if err != nil {
		return fmt.Errorf("cannot access input file: %w", err)
	}
	
	// Check if InputFile is a directory or file based on Recursive flag
	if cfg.Recursive {
		if !inputInfo.IsDir() {
			return fmt.Errorf("input must be a directory when using --recursive: %s", cfg.InputFile)
		}
	} else {
		if inputInfo.IsDir() {
			return fmt.Errorf("input must be a file, not a directory: %s (use --recursive for directories)", cfg.InputFile)
		}
		// Check if it's a regular file (not device, socket, etc.)
		if !inputInfo.Mode().IsRegular() {
			return fmt.Errorf("input must be a regular file: %s", cfg.InputFile)
		}
	}
	
	// Validate OutputDir if specified
	if cfg.OutputDir != "" {
		// Check if OutputDir exists
		outputDirInfo, err := os.Stat(cfg.OutputDir)
		if err == nil {
			// Exists - verify it's a directory
			if !outputDirInfo.IsDir() {
				return fmt.Errorf("output directory path is not a directory: %s", cfg.OutputDir)
			}
			// Check if writable by attempting to create a temp file
			testFile := filepath.Join(cfg.OutputDir, ".write_test")
			f, err := os.Create(testFile)
			if err != nil {
				return fmt.Errorf("output directory is not writable: %s", cfg.OutputDir)
			}
			f.Close()
			os.Remove(testFile)
		} else if os.IsNotExist(err) {
			// Doesn't exist - check if parent exists and is writable
			parentDir := filepath.Dir(cfg.OutputDir)
			if parentDir != cfg.OutputDir {
				parentInfo, err := os.Stat(parentDir)
				if os.IsNotExist(err) {
					return fmt.Errorf("output directory parent does not exist: %s", parentDir)
				}
				if err != nil {
					return fmt.Errorf("cannot access output directory parent: %w", err)
				}
				if !parentInfo.IsDir() {
					return fmt.Errorf("output directory parent is not a directory: %s", parentDir)
				}
			}
		} else {
			return fmt.Errorf("cannot access output directory: %w", err)
		}
	}
	
	// Validate OutputFile if specified
	if cfg.OutputFile != "" {
		// Check if output file already exists
		if _, err := os.Stat(cfg.OutputFile); err == nil {
			// File exists
			if !cfg.Overwrite {
				return fmt.Errorf("output file already exists (use --overwrite to replace): %s", cfg.OutputFile)
			}
		}
		
		// Check if parent directory exists and is writable
		outputDir := filepath.Dir(cfg.OutputFile)
		if outputDir != "" && outputDir != "." {
			parentInfo, err := os.Stat(outputDir)
			if os.IsNotExist(err) {
				return fmt.Errorf("output file parent directory does not exist: %s", outputDir)
			}
			if err != nil {
				return fmt.Errorf("cannot access output file parent directory: %w", err)
			}
			if !parentInfo.IsDir() {
				return fmt.Errorf("output file parent path is not a directory: %s", outputDir)
			}
		}
	}
	
	return nil
}

// validateGlobPattern validates glob pattern syntax
func validateGlobPattern(cfg *ConverterConfig) error {
	if cfg.Glob == "" {
		return nil
	}
	
	// Validate glob pattern syntax using filepath.Match
	// We test with a dummy filename
	_, err := filepath.Match(cfg.Glob, "test.echoreplay")
	if err != nil {
		return fmt.Errorf("invalid glob pattern: %w", err)
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

// ParseByteSize parses a size string with optional unit suffix (K, M, G, T) into bytes.
// Examples: "1000", "1000K", "500M", "10G", "1T"
// Units are case-insensitive and use powers of 1024 (KiB, MiB, GiB, TiB).
// Returns an error if the format is invalid.
func ParseByteSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Match number with optional decimal and unit suffix
	re := regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([kKmMgGtT])?[iI]?[bB]?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid size format: %q (use format like 1000, 500K, 100M, 10G)", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in size: %q", s)
	}

	var multiplier int64 = 1
	if len(matches) > 2 && matches[2] != "" {
		switch strings.ToUpper(matches[2]) {
		case "K":
			multiplier = 1024
		case "M":
			multiplier = 1024 * 1024
		case "G":
			multiplier = 1024 * 1024 * 1024
		case "T":
			multiplier = 1024 * 1024 * 1024 * 1024
		}
	}

	return int64(value * float64(multiplier)), nil
}

// FormatByteSize formats a byte size into a human-readable string with units.
func FormatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(bytes)/float64(div), "KMGT"[exp])
}
