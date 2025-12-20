package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	sessionEventDatabaseName   = "nakama"
	sessionEventCollectionName = "session_events"
)

// SessionFrameDocument represents the MongoDB document structure
type SessionFrameDocument struct {
	ID             primitive.ObjectID                `bson:"_id,omitempty"`
	LobbySessionID string                            `bson:"lobby_session_id"`
	UserID         string                            `bson:"user_id,omitempty"`
	Frame          *telemetry.LobbySessionStateFrame `bson:"frame"`
	EventTypes     []string                          `bson:"event_types,omitempty"`
	Timestamp      time.Time                         `bson:"timestamp"`
	CreatedAt      time.Time                         `bson:"created_at"`
	UpdatedAt      time.Time                         `bson:"updated_at"`
}

// Query resolvers

// LobbySession resolves the lobbySession query
func (r *Resolver) LobbySession(ctx context.Context, id string) (*LobbySession, error) {
	// Basic validation of the lobby session ID to avoid using arbitrary user-controlled values in queries
	if len(id) == 0 || len(id) > 128 {
		return nil, fmt.Errorf("invalid lobby session id")
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_') {
			return nil, fmt.Errorf("invalid lobby session id")
		}
	}

	collection := r.MongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	// Check if any events exist for this session
	filter := bson.M{"lobby_session_id": id}
	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query lobby session: %w", err)
	}

	if count == 0 {
		return nil, nil
	}

	// Get the first and last event to determine created/updated times
	var firstEvent, lastEvent SessionFrameDocument

	opts := options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: 1}})
	if err := collection.FindOne(ctx, filter, opts).Decode(&firstEvent); err != nil {
		return nil, fmt.Errorf("failed to get first event: %w", err)
	}

	opts = options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	if err := collection.FindOne(ctx, filter, opts).Decode(&lastEvent); err != nil {
		return nil, fmt.Errorf("failed to get last event: %w", err)
	}

	return &LobbySession{
		ID:             id,
		LobbySessionID: id,
		TotalEvents:    int(count),
		CreatedAt:      &firstEvent.CreatedAt,
		UpdatedAt:      &lastEvent.UpdatedAt,
	}, nil
}

// SessionEvents resolves the sessionEvents query
func (r *Resolver) SessionEvents(ctx context.Context, lobbySessionID string, limit *int, offset *int) (*SessionEventConnection, error) {
	// Set defaults
	limitVal := 100
	offsetVal := 0
	if limit != nil {
		limitVal = *limit
	}
	if offset != nil {
		offsetVal = *offset
	}

	// Clamp limit
	if limitVal > 1000 {
		limitVal = 1000
	}
	if limitVal < 1 {
		limitVal = 1
	}

	frames, totalCount, err := r.retrieveSessionFramesPaginated(ctx, lobbySessionID, int64(limitVal), int64(offsetVal))
	if err != nil {
		return nil, err
	}

	edges := make([]*SessionEventEdge, 0, len(frames))
	for i, frame := range frames {
		cursor := encodeCursor(offsetVal + i)

		// Convert frame to JSON map
		var frameData map[string]any
		if frame.Frame != nil {
			frameJSON, err := protojson.Marshal(frame.Frame)
			if err == nil {
				// Use standard json package to unmarshal to map
				_ = json.Unmarshal(frameJSON, &frameData)
			}
		}

		edges = append(edges, &SessionEventEdge{
			Cursor: cursor,
			Node: &SessionEvent{
				ID:             frame.ID.Hex(),
				LobbySessionID: frame.LobbySessionID,
				UserID:         &frame.UserID,
				FrameData:      frameData,
				Timestamp:      frame.Timestamp,
				CreatedAt:      frame.CreatedAt,
				UpdatedAt:      frame.UpdatedAt,
			},
		})
	}

	hasNextPage := int64(offsetVal+limitVal) < totalCount
	hasPreviousPage := offsetVal > 0

	var startCursor, endCursor *string
	if len(edges) > 0 {
		startCursor = &edges[0].Cursor
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &SessionEventConnection{
		Edges: edges,
		PageInfo: &PageInfo{
			HasNextPage:     hasNextPage,
			HasPreviousPage: hasPreviousPage,
			StartCursor:     startCursor,
			EndCursor:       endCursor,
		},
		TotalCount: int(totalCount),
	}, nil
}

// retrieveSessionFramesPaginated retrieves session frames with pagination
func (r *Resolver) retrieveSessionFramesPaginated(ctx context.Context, sessionID string, limit, offset int64) ([]*SessionFrameDocument, int64, error) {
	collection := r.MongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	filter := bson.M{"lobby_session_id": sessionID}

	totalCount, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count session frames: %w", err)
	}

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

// Health resolves the health query
func (r *Resolver) Health(ctx context.Context) (*HealthStatus, error) {
	dbStatus := "connected"
	if err := r.MongoClient.Ping(ctx, nil); err != nil {
		dbStatus = "disconnected"
	}

	return &HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Database:  dbStatus,
	}, nil
}

// Mutation resolvers

// StoreSessionEvent resolves the storeSessionEvent mutation
func (r *Resolver) StoreSessionEvent(ctx context.Context, input StoreSessionEventInput) (*StoreSessionEventPayload, error) {
	collection := r.MongoClient.Database(sessionEventDatabaseName).Collection(sessionEventCollectionName)

	// Convert input.FrameData (map[string]any) to LobbySessionStateFrame
	frameDataBytes, err := json.Marshal(input.FrameData)
	if err != nil {
		errMsg := fmt.Sprintf("failed to serialize frame data: %v", err)
		return &StoreSessionEventPayload{
			Success: false,
			Error:   &errMsg,
		}, nil
	}

	frame := &telemetry.LobbySessionStateFrame{}
	if err := protojson.Unmarshal(frameDataBytes, frame); err != nil {
		errMsg := fmt.Sprintf("failed to parse frame data: %v", err)
		return &StoreSessionEventPayload{
			Success: false,
			Error:   &errMsg,
		}, nil
	}

	userID := ""
	if input.UserID != nil {
		userID = *input.UserID
	}

	// Extract event types
	eventTypes := make([]string, 0, len(frame.GetEvents()))
	for _, evt := range frame.GetEvents() {
		if evt != nil && evt.Event != nil {
			// Get the event type name from the oneof
			eventType := fmt.Sprintf("%T", evt.Event)
			eventTypes = append(eventTypes, eventType)
		}
	}

	now := time.Now().UTC()
	doc := &SessionFrameDocument{
		ID:             primitive.NewObjectID(),
		LobbySessionID: input.LobbySessionID,
		UserID:         userID,
		Frame:          frame,
		EventTypes:     eventTypes,
		Timestamp:      frame.Timestamp.AsTime(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err = collection.InsertOne(ctx, doc)
	if err != nil {
		errMsg := fmt.Sprintf("failed to store frame: %v", err)
		return &StoreSessionEventPayload{
			Success: false,
			Error:   &errMsg,
		}, nil
	}

	return &StoreSessionEventPayload{
		Success: true,
		Event: &SessionEvent{
			ID:             doc.ID.Hex(),
			LobbySessionID: doc.LobbySessionID,
			UserID:         &doc.UserID,
			FrameData:      input.FrameData,
			Timestamp:      doc.Timestamp,
			CreatedAt:      doc.CreatedAt,
			UpdatedAt:      doc.UpdatedAt,
		},
	}, nil
}

// LobbySession field resolvers

// Events resolves the events field on LobbySession
func (r *Resolver) LobbySessionEvents(ctx context.Context, obj *LobbySession, limit *int, offset *int) (*SessionEventConnection, error) {
	return r.SessionEvents(ctx, obj.LobbySessionID, limit, offset)
}

// Helper functions

func encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

// Unused but kept for potential future use
var _ = mongo.Client{}

// GraphQL model types

type LobbySession struct {
	ID             string     `json:"id"`
	LobbySessionID string     `json:"lobbySessionId"`
	TotalEvents    int        `json:"totalEvents"`
	CreatedAt      *time.Time `json:"createdAt"`
	UpdatedAt      *time.Time `json:"updatedAt"`
}

type SessionEvent struct {
	ID             string         `json:"id"`
	LobbySessionID string         `json:"lobbySessionId"`
	UserID         *string        `json:"userId"`
	FrameData      map[string]any `json:"frameData"`
	Timestamp      time.Time      `json:"timestamp"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type SessionEventConnection struct {
	Edges      []*SessionEventEdge `json:"edges"`
	PageInfo   *PageInfo           `json:"pageInfo"`
	TotalCount int                 `json:"totalCount"`
}

type SessionEventEdge struct {
	Node   *SessionEvent `json:"node"`
	Cursor string        `json:"cursor"`
}

type PageInfo struct {
	HasNextPage     bool    `json:"hasNextPage"`
	HasPreviousPage bool    `json:"hasPreviousPage"`
	StartCursor     *string `json:"startCursor"`
	EndCursor       *string `json:"endCursor"`
}

type HealthStatus struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Database  string    `json:"database"`
}

type StoreSessionEventInput struct {
	LobbySessionID string         `json:"lobbySessionId"`
	UserID         *string        `json:"userId"`
	FrameData      map[string]any `json:"frameData"`
}

type StoreSessionEventPayload struct {
	Success bool          `json:"success"`
	Event   *SessionEvent `json:"event"`
	Error   *string       `json:"error"`
}
