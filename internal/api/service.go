package api

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Config represents the configuration for the session events service
type Config struct {
	// MongoDB configuration
	MongoURI       string `json:"mongo_uri" yaml:"mongo_uri"`
	DatabaseName   string `json:"database_name" yaml:"database_name"`
	CollectionName string `json:"collection_name" yaml:"collection_name"`

	// HTTP server configuration
	ServerAddress string `json:"server_address" yaml:"server_address"`

	// JWT configuration
	JWTSecret string `json:"jwt_secret" yaml:"jwt_secret"`

	// AMQP configuration
	AMQPURI       string `json:"amqp_uri" yaml:"amqp_uri"`
	AMQPQueueName string `json:"amqp_queue_name" yaml:"amqp_queue_name"`
	AMQPEnabled   bool   `json:"amqp_enabled" yaml:"amqp_enabled"`

	// Capture storage configuration
	CaptureDir       string `json:"capture_dir" yaml:"capture_dir"`
	CaptureRetention string `json:"capture_retention" yaml:"capture_retention"` // Duration string
	CaptureMaxSize   int64  `json:"capture_max_size" yaml:"capture_max_size"`   // Max bytes

	// Rate limiting
	MaxStreamHz int `json:"max_stream_hz" yaml:"max_stream_hz"`

	// Metrics
	MetricsAddr string `json:"metrics_addr" yaml:"metrics_addr"`

	// Optional timeouts
	MongoTimeout  time.Duration `json:"mongo_timeout" yaml:"mongo_timeout"`
	ServerTimeout time.Duration `json:"server_timeout" yaml:"server_timeout"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	// Check for environment variables with EVR_APISERVER_ prefix
	amqpURI := os.Getenv("EVR_APISERVER_AMQP_URI")
	if amqpURI == "" {
		amqpURI = "amqp://guest:guest@localhost:5672/"
	}

	amqpEnabled := os.Getenv("EVR_APISERVER_AMQP_ENABLED") == "true"

	mongoURI := os.Getenv("EVR_APISERVER_MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	serverAddress := os.Getenv("EVR_APISERVER_SERVER_ADDRESS")
	if serverAddress == "" {
		serverAddress = ":8080"
	}

	jwtSecret := os.Getenv("EVR_APISERVER_JWT_SECRET")

	return &Config{
		MongoURI:         mongoURI,
		DatabaseName:     sessionEventDatabaseName,
		CollectionName:   sessionEventCollectionName,
		ServerAddress:    serverAddress,
		JWTSecret:        jwtSecret,
		AMQPURI:          amqpURI,
		AMQPQueueName:    amqp.DefaultQueueName,
		AMQPEnabled:      amqpEnabled,
		CaptureDir:       "./captures",
		CaptureRetention: "168h",
		CaptureMaxSize:   10 * 1024 * 1024 * 1024, // 10GB
		MaxStreamHz:      60,
		MetricsAddr:      "",
		MongoTimeout:     10 * time.Second,
		ServerTimeout:    30 * time.Second,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.MongoURI == "" {
		return fmt.Errorf("mongo_uri is required")
	}
	if c.DatabaseName == "" {
		return fmt.Errorf("database_name is required")
	}
	if c.CollectionName == "" {
		return fmt.Errorf("collection_name is required")
	}
	if c.ServerAddress == "" {
		return fmt.Errorf("server_address is required")
	}
	if c.JWTSecret == "" {
		return fmt.Errorf("jwt_secret is required")
	}
	if c.AMQPEnabled && c.AMQPURI == "" {
		return fmt.Errorf("amqp_uri is required when AMQP is enabled")
	}
	return nil
}

// Service represents the complete session events service
type Service struct {
	config        *Config
	mongoClient   *mongo.Client
	server        *Server
	amqpPublisher *amqp.Publisher
	logger        Logger
}

// NewService creates a new session events service
func NewService(config *Config, logger Logger) (*Service, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if logger == nil {
		logger = &DefaultLogger{}
	}

	return &Service{
		config: config,
		logger: logger,
	}, nil
}

// Initialize initializes the service (connects to MongoDB, creates indexes, etc.)
func (s *Service) Initialize(ctx context.Context) error {
	// Connect to MongoDB
	mongoClient, err := s.connectMongoDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	s.mongoClient = mongoClient

	// Create indexes
	if err := s.createIndexes(ctx); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	// Initialize AMQP publisher if enabled
	if s.config.AMQPEnabled {
		publisher, err := amqp.NewPublisher(&amqp.Config{
			URI:       s.config.AMQPURI,
			QueueName: s.config.AMQPQueueName,
		}, s.logger)
		if err != nil {
			return fmt.Errorf("failed to create AMQP publisher: %w", err)
		}

		if err := publisher.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect to AMQP: %w", err)
		}

		s.amqpPublisher = publisher
		s.logger.Info("AMQP publisher initialized", "queue", s.config.AMQPQueueName)
	}

	// Create HTTP server
	s.server = NewServer(s.mongoClient, s.logger, s.config.JWTSecret)

	// Set the AMQP publisher on the server if available
	if s.amqpPublisher != nil {
		s.server.SetAMQPPublisher(s.amqpPublisher)
	}

	s.logger.Info("Session events service initialized successfully")
	return nil
}

// connectMongoDB establishes a connection to MongoDB
func (s *Service) connectMongoDB(ctx context.Context) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.MongoTimeout)
	defer cancel()

	clientOptions := options.Client().ApplyURI(s.config.MongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	s.logger.Info("Connected to MongoDB", "uri", s.config.MongoURI)
	return client, nil
}

// createIndexes creates necessary database indexes
func (s *Service) createIndexes(ctx context.Context) error {
	collection := s.mongoClient.Database(s.config.DatabaseName).Collection(s.config.CollectionName)

	ctx, cancel := context.WithTimeout(ctx, s.config.MongoTimeout)
	defer cancel()

	// Create index on lobby_session_id for faster queries
	sessionIDIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "lobby_session_id", Value: 1},
		},
	}

	_, err := collection.Indexes().CreateOne(ctx, sessionIDIndex)
	if err != nil {
		return fmt.Errorf("failed to create lobby_session_id index: %w", err)
	}

	// Create compound index on lobby_session_id and timestamp for sorted queries
	timestampIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "lobby_session_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	}

	_, err = collection.Indexes().CreateOne(ctx, timestampIndexModel)
	if err != nil {
		return fmt.Errorf("failed to create lobby_session_id+timestamp index: %w", err)
	}

	// Create index on event_types for event type queries
	eventTypesIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "event_types", Value: 1},
		},
	}

	_, err = collection.Indexes().CreateOne(ctx, eventTypesIndex)
	if err != nil {
		return fmt.Errorf("failed to create event_types index: %w", err)
	}

	// Create compound index on lobby_session_id and event_types for filtered queries
	compoundEventIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "lobby_session_id", Value: 1},
			{Key: "event_types", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	}

	_, err = collection.Indexes().CreateOne(ctx, compoundEventIndex)
	if err != nil {
		return fmt.Errorf("failed to create lobby_session_id+event_types+timestamp index: %w", err)
	}

	s.logger.Debug("Created database indexes")
	return nil
}

// Start starts the service
func (s *Service) Start(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("service not initialized, call Initialize() first")
	}

	s.logger.Info("Starting session events service", "address", s.config.ServerAddress)
	return s.server.StartWithContext(ctx, s.config.ServerAddress)
}

// Stop stops the service and closes connections
func (s *Service) Stop(ctx context.Context) error {
	var errs []error

	// Close AMQP publisher
	if s.amqpPublisher != nil {
		if err := s.amqpPublisher.Close(); err != nil {
			s.logger.Error("Failed to close AMQP publisher", "error", err)
			errs = append(errs, err)
		}
	}

	// Disconnect MongoDB
	if s.mongoClient != nil {
		if err := s.mongoClient.Disconnect(ctx); err != nil {
			s.logger.Error("Failed to disconnect MongoDB client", "error", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping service: %v", errs)
	}

	s.logger.Info("Session events service stopped")
	return nil
}

// GetAMQPPublisher returns the AMQP publisher instance
func (s *Service) GetAMQPPublisher() *amqp.Publisher {
	return s.amqpPublisher
}

// GetServer returns the HTTP server instance
func (s *Service) GetServer() *Server {
	return s.server
}

// GetMongoClient returns the MongoDB client instance
func (s *Service) GetMongoClient() *mongo.Client {
	return s.mongoClient
}
