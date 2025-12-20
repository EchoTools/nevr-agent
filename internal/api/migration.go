package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MigrationStats holds statistics about the migration process
type MigrationStats struct {
	TotalDocuments    int64
	MigratedDocuments int64
	SkippedDocuments  int64
	FailedDocuments   int64
	StartTime         time.Time
	EndTime           time.Time
}

// MigrateSchema performs a one-time migration from the legacy schema to v3 schema
// This adds _id, timestamp, created_at, and updated_at fields to existing documents
func MigrateSchema(ctx context.Context, mongoClient *mongo.Client, logger Logger) (*MigrationStats, error) {
	if logger == nil {
		logger = &DefaultLogger{}
	}

	stats := &MigrationStats{
		StartTime: time.Now(),
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	// Count total documents
	totalCount, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to count documents: %w", err)
	}
	stats.TotalDocuments = totalCount

	logger.Info("Starting schema migration", "total_documents", totalCount)

	// Find documents without the new fields (created_at is used as marker)
	filter := bson.M{
		"$or": []bson.M{
			{"created_at": bson.M{"$exists": false}},
			{"_id": bson.M{"$type": "string"}}, // Documents with string _id need migration
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents for migration: %w", err)
	}
	defer cursor.Close(ctx)

	batchSize := 100
	var batch []mongo.WriteModel

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			logger.Error("Failed to decode document", "error", err)
			stats.FailedDocuments++
			continue
		}

		// Extract timestamp from frame data if possible
		timestamp := extractTimestampFromFrame(doc)
		if timestamp.IsZero() {
			timestamp = time.Now().UTC()
		}

		// Prepare update
		oldID := doc["_id"]
		newID := primitive.NewObjectID()

		update := bson.M{
			"$set": bson.M{
				"timestamp":  timestamp,
				"created_at": timestamp,
				"updated_at": time.Now().UTC(),
			},
		}

		// If _id is not already an ObjectID, we need to delete and reinsert
		if _, ok := oldID.(primitive.ObjectID); !ok {
			// Create new document with ObjectID
			newDoc := bson.M{
				"_id":              newID,
				"lobby_session_id": doc["lobby_session_id"],
				"user_id":          doc["user_id"],
				"frame":            doc["frame"],
				"timestamp":        timestamp,
				"created_at":       timestamp,
				"updated_at":       time.Now().UTC(),
			}

			// Delete old document
			deleteModel := mongo.NewDeleteOneModel().SetFilter(bson.M{"_id": oldID})
			batch = append(batch, deleteModel)

			// Insert new document
			insertModel := mongo.NewInsertOneModel().SetDocument(newDoc)
			batch = append(batch, insertModel)
		} else {
			// Just update existing document
			updateModel := mongo.NewUpdateOneModel().
				SetFilter(bson.M{"_id": oldID}).
				SetUpdate(update)
			batch = append(batch, updateModel)
		}

		// Execute batch when full
		if len(batch) >= batchSize {
			result, err := collection.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false))
			if err != nil {
				logger.Error("Batch write failed", "error", err)
				stats.FailedDocuments += int64(len(batch))
			} else {
				stats.MigratedDocuments += result.ModifiedCount + result.InsertedCount
			}
			batch = batch[:0]
		}
	}

	// Execute remaining batch
	if len(batch) > 0 {
		result, err := collection.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false))
		if err != nil {
			logger.Error("Final batch write failed", "error", err)
			stats.FailedDocuments += int64(len(batch))
		} else {
			stats.MigratedDocuments += result.ModifiedCount + result.InsertedCount
		}
	}

	stats.EndTime = time.Now()
	stats.SkippedDocuments = stats.TotalDocuments - stats.MigratedDocuments - stats.FailedDocuments

	logger.Info("Schema migration completed",
		"migrated", stats.MigratedDocuments,
		"skipped", stats.SkippedDocuments,
		"failed", stats.FailedDocuments,
		"duration", stats.EndTime.Sub(stats.StartTime),
	)

	return stats, nil
}

// extractTimestampFromFrame attempts to extract a timestamp from the frame data JSON
func extractTimestampFromFrame(doc bson.M) time.Time {
	frameData, ok := doc["frame"].(string)
	if !ok || frameData == "" {
		return time.Time{}
	}

	var frame map[string]any
	if err := json.Unmarshal([]byte(frameData), &frame); err != nil {
		return time.Time{}
	}

	// Try to extract timestamp from common fields
	// Look for session.timestamp or timestamp field
	if session, ok := frame["session"].(map[string]any); ok {
		if ts, ok := session["timestamp"].(float64); ok {
			return time.Unix(int64(ts), 0).UTC()
		}
		if ts, ok := session["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				return t
			}
		}
	}

	// Try top-level timestamp
	if ts, ok := frame["timestamp"].(float64); ok {
		return time.Unix(int64(ts), 0).UTC()
	}
	if ts, ok := frame["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}

	return time.Time{}
}

// ValidateMigration checks that all documents have been properly migrated
func ValidateMigration(ctx context.Context, mongoClient *mongo.Client, logger Logger) error {
	if logger == nil {
		logger = &DefaultLogger{}
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	// Check for documents missing new fields
	filter := bson.M{
		"$or": []bson.M{
			{"created_at": bson.M{"$exists": false}},
			{"timestamp": bson.M{"$exists": false}},
			{"updated_at": bson.M{"$exists": false}},
		},
	}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to validate migration: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("migration incomplete: %d documents still missing required fields", count)
	}

	logger.Info("Migration validation passed")
	return nil
}
