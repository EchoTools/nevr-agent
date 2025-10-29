package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// StoreSessionEvent stores a session event to MongoDB
func StoreSessionEvent(ctx context.Context, mongoClient *mongo.Client, event *SessionEvent) error {
	if mongoClient == nil {
		return fmt.Errorf("mongo client is nil")
	}

	if uuid.FromStringOrNil(event.LobbySessionUUID).IsNil() {
		return fmt.Errorf("lobby_session_id is invalid")
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := collection.InsertOne(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to insert session event: %w", err)
	}

	return nil
}

// RetrieveSessionEventsByMatchID retrieves all session events for a given match ID from MongoDB
func RetrieveSessionEventsByMatchID(ctx context.Context, mongoClient *mongo.Client, matchID string) ([]*SessionEvent, error) {
	if mongoClient == nil {
		return nil, fmt.Errorf("mongo client is nil")
	}

	if matchID == "" {
		return nil, fmt.Errorf("lobby_session_id is required")
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create filter for lobby_session_id
	filter := bson.M{"lobby_session_id": matchID}

	// Sort by timestamp ascending
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query session events: %w", err)
	}
	defer cursor.Close(ctx)

	var events []*SessionEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("failed to decode session events: %w", err)
	}

	return events, nil
}
