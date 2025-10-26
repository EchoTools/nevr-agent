package api

import (
	"context"
	"fmt"
	"time"

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

	// Optional timeouts
	MongoTimeout  time.Duration `json:"mongo_timeout" yaml:"mongo_timeout"`
	ServerTimeout time.Duration `json:"server_timeout" yaml:"server_timeout"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		MongoURI:       "mongodb://localhost:27017",
		DatabaseName:   sessionEventDatabaseName,
		CollectionName: sessionEventCollectionName,
		ServerAddress:  ":8080",
		MongoTimeout:   10 * time.Second,
		ServerTimeout:  30 * time.Second,
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
	return nil
}

// Service represents the complete session events service
type Service struct {
	config      *Config
	mongoClient *mongo.Client
	server      *Server
	logger      Logger
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

	// Create HTTP server
	s.server = NewServer(s.mongoClient, s.logger)

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

	// Create index on match_id for faster queries
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "match_id", Value: 1},
		},
	}

	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return fmt.Errorf("failed to create match_id index: %w", err)
	}

	// Create compound index on match_id and timestamp for sorted queries
	timestampIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "match_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	}

	_, err = collection.Indexes().CreateOne(ctx, timestampIndexModel)
	if err != nil {
		return fmt.Errorf("failed to create match_id+timestamp index: %w", err)
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
	if s.mongoClient != nil {
		if err := s.mongoClient.Disconnect(ctx); err != nil {
			s.logger.Error("Failed to disconnect MongoDB client", "error", err)
			return err
		}
	}

	s.logger.Info("Session events service stopped")
	return nil
}

// GetServer returns the HTTP server instance
func (s *Service) GetServer() *Server {
	return s.server
}

// GetMongoClient returns the MongoDB client instance
func (s *Service) GetMongoClient() *mongo.Client {
	return s.mongoClient
}
