package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database schema migrations",
		Long: `Migrate runs schema migrations on the MongoDB database.

This command connects to MongoDB and applies any pending schema migrations
to ensure the database structure is up to date.`,
		Example: `  # Run migration with default MongoDB URI
  agent migrate

  # Run migration with custom MongoDB URI
  agent migrate --mongo-uri mongodb://user:pass@localhost:27017/dbname`,
		RunE: runMigrate,
	}

	cmd.Flags().String("mongo-uri", "mongodb://localhost:27017", "MongoDB connection URI")
	viper.BindPFlag("migrate.mongo-uri", cmd.Flags().Lookup("mongo-uri"))

	return cmd
}

func runMigrate(cmd *cobra.Command, args []string) error {
	// Get MongoDB URI from flag or environment
	mongoURI := viper.GetString("migrate.mongo-uri")
	if mongoURI == "" {
		mongoURI = os.Getenv("EVR_APISERVER_MONGO_URI")
	}
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	logger.Info("Starting schema migration")
	fmt.Printf("Connecting to MongoDB: %s\n", mongoURI)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, cancelling migration...")
		cancel()
	}()

	// Connect to MongoDB
	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() {
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer disconnectCancel()
		client.Disconnect(disconnectCtx)
	}()

	// Ping MongoDB to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}
	fmt.Println("Connected to MongoDB successfully")

	// Create logger
	apiLogger := &api.DefaultLogger{}

	// Run migration
	fmt.Println("Starting schema migration...")
	stats, err := api.MigrateSchema(ctx, client, apiLogger)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Print statistics
	fmt.Println("\n=== Migration Statistics ===")
	fmt.Printf("Total documents:    %d\n", stats.TotalDocuments)
	fmt.Printf("Migrated documents: %d\n", stats.MigratedDocuments)
	fmt.Printf("Skipped documents:  %d\n", stats.SkippedDocuments)
	fmt.Printf("Failed documents:   %d\n", stats.FailedDocuments)
	fmt.Printf("Duration:           %v\n", stats.EndTime.Sub(stats.StartTime))

	// Validate migration
	fmt.Println("\nValidating migration...")
	if err := api.ValidateMigration(ctx, client, apiLogger); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Println("\nMigration completed successfully!")
	return nil
}
