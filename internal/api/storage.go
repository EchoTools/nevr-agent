package api

import (
	"context"
	"fmt"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"github.com/gofrs/uuid/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionFrameDocument represents a LobbySessionStateFrame stored in MongoDB
type SessionFrameDocument struct {
	ID             primitive.ObjectID                `bson:"_id,omitempty"`
	LobbySessionID string                            `bson:"lobby_session_id"`
	UserID         string                            `bson:"user_id,omitempty"`
	Frame          *telemetry.LobbySessionStateFrame `bson:"frame"`
	EventTypes     []string                          `bson:"event_types,omitempty"` // For indexing/querying
	Timestamp      time.Time                         `bson:"timestamp"`
	CreatedAt      time.Time                         `bson:"created_at"`
	UpdatedAt      time.Time                         `bson:"updated_at"`
}

// StoreSessionFrame stores a session frame to MongoDB
func StoreSessionFrame(ctx context.Context, mongoClient *mongo.Client, lobbySessionID, userID string, frame *telemetry.LobbySessionStateFrame) error {
	if mongoClient == nil {
		return fmt.Errorf("mongo client is nil")
	}

	if uuid.FromStringOrNil(lobbySessionID).IsNil() {
		return fmt.Errorf("lobby_session_id is invalid")
	}

	if frame == nil {
		return fmt.Errorf("frame is nil")
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Extract event types for indexing
	eventTypes := make([]string, 0, len(frame.GetEvents()))
	for _, evt := range frame.GetEvents() {
		if evt != nil && evt.Event != nil {
			// Get the event type name from the oneof
			eventType := fmt.Sprintf("%T", evt.Event)
			eventTypes = append(eventTypes, eventType)
		}
	}

	// Set timestamps
	now := time.Now().UTC()
	if frame.Timestamp == nil {
		frame.Timestamp = timestamppb.New(now)
	}

	doc := &SessionFrameDocument{
		ID:             primitive.NewObjectID(),
		LobbySessionID: lobbySessionID,
		UserID:         userID,
		Frame:          frame,
		EventTypes:     eventTypes,
		Timestamp:      frame.Timestamp.AsTime(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err := collection.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to insert session frame: %w", err)
	}

	return nil
}

// RetrieveSessionFramesBySessionID retrieves all session frames for a given session ID from MongoDB
func RetrieveSessionFramesBySessionID(ctx context.Context, mongoClient *mongo.Client, sessionID string) ([]*SessionFrameDocument, error) {
	if mongoClient == nil {
		return nil, fmt.Errorf("mongo client is nil")
	}

	if sessionID == "" {
		return nil, fmt.Errorf("lobby_session_id is required")
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create filter for lobby_session_id
	filter := bson.M{"lobby_session_id": sessionID}

	// Sort by timestamp ascending
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query session frames: %w", err)
	}
	defer cursor.Close(ctx)

	var frames []*SessionFrameDocument
	if err := cursor.All(ctx, &frames); err != nil {
		return nil, fmt.Errorf("failed to decode session frames: %w", err)
	}

	return frames, nil
}

// RetrieveSessionFramesPaginated retrieves session frames with pagination support
func RetrieveSessionFramesPaginated(ctx context.Context, mongoClient *mongo.Client, sessionID string, eventType *string, limit, offset int64) ([]*SessionFrameDocument, int64, error) {
	if mongoClient == nil {
		return nil, 0, fmt.Errorf("mongo client is nil")
	}

	if sessionID == "" {
		return nil, 0, fmt.Errorf("lobby_session_id is required")
	}

	collection := mongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create filter for lobby_session_id
	filter := bson.M{"lobby_session_id": sessionID}

	// Add event type filter if provided
	if eventType != nil && *eventType != "" {
		filter["event_types"] = *eventType
	}

	// Get total count
	totalCount, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count session frames: %w", err)
	}

	// Set defaults for pagination
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	// Sort by timestamp ascending with pagination
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query session frames: %w", err)
	}
	defer cursor.Close(ctx)

	var frames []*SessionFrameDocument
	if err := cursor.All(ctx, &frames); err != nil {
		return nil, 0, fmt.Errorf("failed to decode session frames: %w", err)
	}

	return frames, totalCount, nil
}

// FrameToJSON converts a LobbySessionStateFrame to JSON bytes
func FrameToJSON(frame *telemetry.LobbySessionStateFrame) ([]byte, error) {
	return protojson.Marshal(frame)
}
