package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/echotools/nevr-agent/v4/internal/amqp"
	"github.com/echotools/nevr-agent/v4/internal/api/graph"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/protobuf/encoding/protojson"
)

var jsonMarshaler = &protojson.MarshalOptions{
	UseProtoNames:   false,
	UseEnumNumbers:  true,
	EmitUnpopulated: true,
	Indent:          "  ",
}

// Server represents the HTTP server for session events
type Server struct {
	mongoClient     *mongo.Client
	router          *mux.Router
	logger          Logger
	graphqlResolver *graph.Resolver
	corsHandler     *cors.Cors
	amqpPublisher   *amqp.Publisher
	jwtSecret       string
	nodeID          string
	frameCount      atomic.Int64
	streamHub       *StreamHub
	storageManager  *StorageManager
	matchRetrieval  *MatchRetrievalHandler
}

// Logger interface for abstracting logging
type Logger interface {
	Debug(msg string, fields ...any)
	Info(msg string, fields ...any)
	Error(msg string, fields ...any)
	Warn(msg string, fields ...any)
}

// DefaultLogger provides a simple logger implementation
type DefaultLogger struct{}

func (l *DefaultLogger) Debug(msg string, fields ...any) {
	log.Printf("[DEBUG] %s %v", msg, fields)
}

func (l *DefaultLogger) Info(msg string, fields ...any) {
	log.Printf("[INFO] %s %v", msg, fields)
}

func (l *DefaultLogger) Error(msg string, fields ...any) {
	log.Printf("[ERROR] %s %v", msg, fields)
}

func (l *DefaultLogger) Warn(msg string, fields ...any) {
	log.Printf("[WARN] %s %v", msg, fields)
}

// SetAMQPPublisher sets the AMQP publisher for the server
func (s *Server) SetAMQPPublisher(publisher *amqp.Publisher) {
	s.amqpPublisher = publisher
}

// NewServer creates a new session events HTTP server
func NewServer(mongoClient *mongo.Client, logger Logger, jwtSecret string) *Server {
	return NewServerWithStorage(mongoClient, logger, jwtSecret, nil, 60, "")
}

// NewServerWithStorage creates a new session events HTTP server with storage support
func NewServerWithStorage(mongoClient *mongo.Client, logger Logger, jwtSecret string, storage *StorageManager, maxFrameRate int, nodeID string) *Server {
	if logger == nil {
		logger = &DefaultLogger{}
	}
	if maxFrameRate <= 0 {
		maxFrameRate = 60
	}
	if nodeID == "" {
		if hostname, err := os.Hostname(); err == nil {
			nodeID = hostname
		} else {
			nodeID = "default-node"
		}
	}

	router := mux.NewRouter()
	router.StrictSlash(true) // Handle trailing slashes consistently

	s := &Server{
		mongoClient:     mongoClient,
		router:          router,
		logger:          logger,
		graphqlResolver: graph.NewResolver(mongoClient),
		corsHandler:     createCORSHandler(),
		jwtSecret:       jwtSecret,
		nodeID:          nodeID,
		storageManager:  storage,
		streamHub:       NewStreamHub(storage, logger, nil, maxFrameRate, nil),
	}

	// Create match retrieval handler if storage is available
	if storage != nil {
		s.matchRetrieval = NewMatchRetrievalHandler(storage, logger, "")
	}

	s.setupRoutes()
	return s
}

// createCORSHandler creates a CORS handler with configurable origins
func createCORSHandler() *cors.Cors {
	// Get allowed origins from environment variable
	originsEnv := os.Getenv("EVR_APISERVER_CORS_ORIGINS")
	var allowedOrigins []string

	if originsEnv != "" {
		allowedOrigins = strings.Split(originsEnv, ",")
		for i, origin := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(origin)
		}
	} else {
		// Default to allowing all origins in development
		allowedOrigins = []string{"*"}
	}

	return cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Node-ID", "X-User-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any major browser
	})
}

// setupRoutes configures the HTTP routes with versioned API support
func (s *Server) setupRoutes() {
	// Health check (unversioned)
	s.router.HandleFunc("/health", s.healthHandler).Methods("GET")

	// ============================================
	// v1 API - Legacy endpoints (backward compatible)
	// ============================================
	v1 := s.router.PathPrefix("/v1").Subrouter()
	v1.Use(s.corsOptionsMiddleware)
	v1.HandleFunc("/lobby-session-events/{lobby_session_id}", s.getSessionEventsHandlerV1).Methods("GET")

	// Legacy routes without version prefix (deprecated, redirects to v1)
	s.router.Use(s.corsOptionsMiddleware)
	s.router.HandleFunc("/lobby-session-events/{lobby_session_id}", s.getSessionEventsHandlerV1).Methods("GET")

	// ============================================
	// v3 API - New GraphQL and REST endpoints
	// ============================================
	v3 := s.router.PathPrefix("/v3").Subrouter()
	v3.Use(s.corsOptionsMiddleware)

	// GraphQL endpoint
	v3.Handle("/query", s.graphqlResolver.Handler()).Methods("POST")
	v3.Handle("/graphql", s.graphqlResolver.Handler()).Methods("POST")

	// GraphQL Playground (development tool)
	v3.Handle("/playground", graph.PlaygroundHandler("/v3/query")).Methods("GET")

	// v3 REST endpoints - GET only (events are received via WebSocket)
	v3.HandleFunc("/lobby-session-events/{lobby_session_id}", s.getSessionEventsHandlerV3).Methods("GET")

	// WebSocket stream endpoint with JWT authentication (primary way to receive events)
	v3.HandleFunc("/stream", JWTMiddleware(s.jwtSecret, s.WebSocketStreamHandler)).Methods("GET")

	// Shorter WebSocket endpoint alias
	s.router.HandleFunc("/ws", JWTMiddleware(s.jwtSecret, s.WebSocketStreamHandler)).Methods("GET")

	// Register StreamHub routes for match streaming
	s.streamHub.RegisterRoutes(s.router)

	// Register match retrieval routes if storage is available
	if s.matchRetrieval != nil {
		s.matchRetrieval.RegisterRoutes(s.router)
	}

	// Add a NotFoundHandler for debugging unmatched routes
	s.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Warn("Route not found", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "404 page not found", http.StatusNotFound)
	})

	// Add a MethodNotAllowedHandler for debugging method mismatches
	s.router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Warn("Method not allowed", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "405 method not allowed", http.StatusMethodNotAllowed)
	})
}

// corsOptionsMiddleware handles CORS preflight OPTIONS requests
func (s *Server) corsOptionsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Node-ID, X-User-ID")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getSessionEventsHandlerV1 handles GET requests to retrieve session events (v1 legacy format)
func (s *Server) getSessionEventsHandlerV1(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	sessionID := vars["lobby_session_id"]

	if sessionID == "" {
		http.Error(w, "lobby_session_id is required", http.StatusBadRequest)
		return
	}

	// Retrieve frames from MongoDB
	frames, err := RetrieveSessionFramesBySessionID(ctx, s.mongoClient, sessionID)
	if err != nil {
		s.logger.Error("Failed to retrieve session frames", "error", err, "lobby_session_id", sessionID)
		http.Error(w, "Failed to retrieve session frames", http.StatusInternalServerError)
		return
	}

	// Return response in v1 legacy format (convert frames to JSON)
	entries := make([]*SessionEventResponseEntry, 0, len(frames))
	for _, f := range frames {
		frameJSON, err := FrameToJSON(f.Frame)
		if err != nil {
			s.logger.Warn("Failed to convert frame to JSON", "error", err)
			continue
		}
		entry := &SessionEventResponseEntry{
			UserID:    f.UserID,
			FrameData: (json.RawMessage)(frameJSON),
		}
		entries = append(entries, entry)
	}

	response := &SessionResponse{
		LobbySessionUUID: sessionID,
		Events:           entries,
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug("Retrieved session frames (v1)", "lobby_session_id", sessionID, "count", len(frames))
}

// getSessionEventsHandlerV3 handles GET requests to retrieve session events (v3 format with full schema)
func (s *Server) getSessionEventsHandlerV3(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	sessionID := vars["lobby_session_id"]

	if sessionID == "" {
		http.Error(w, "lobby_session_id is required", http.StatusBadRequest)
		return
	}

	// Parse optional event_type query parameter
	var eventType *string
	if et := r.URL.Query().Get("event_type"); et != "" {
		eventType = &et
	}

	// Retrieve frames from MongoDB with pagination
	frames, totalCount, err := RetrieveSessionFramesPaginated(ctx, s.mongoClient, sessionID, eventType, 100, 0)
	if err != nil {
		s.logger.Error("Failed to retrieve session frames", "error", err, "lobby_session_id", sessionID)
		http.Error(w, "Failed to retrieve session frames", http.StatusInternalServerError)
		return
	}

	// Return response in v3 format (full schema with timestamps)
	response := &SessionResponseV3{
		LobbySessionUUID: sessionID,
		Frames:           frames,
		TotalCount:       totalCount,
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug("Retrieved session frames (v3)", "lobby_session_id", sessionID, "count", len(frames))
}

// healthHandler handles health check requests
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Check MongoDB connection
	if err := s.mongoClient.Ping(ctx, nil); err != nil {
		s.logger.Error("MongoDB health check failed", "error", err)
		http.Error(w, "Database connection failed", http.StatusServiceUnavailable)
		return
	}

	response := map[string]string{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ServeHTTP implements the http.Handler interface with CORS support
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.corsHandler.Handler(s.router).ServeHTTP(w, r)
}

// Start starts the HTTP server on the specified address
func (s *Server) Start(address string) error {
	s.logger.Info("Starting session events HTTP server", "address", address)

	server := &http.Server{
		Addr:         address,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return server.ListenAndServe()
}

// StartWithContext starts the HTTP server with context for graceful shutdown
func (s *Server) StartWithContext(ctx context.Context, address string) error {
	s.logger.Info("Starting session events HTTP server with context", "address", address)

	server := &http.Server{
		Addr:         address,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start frame counter logging goroutine
	go s.logFrameStats(ctx)

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Server failed to start", "error", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("Server shutdown failed", "error", err)
		return err
	}

	s.logger.Info("Server shutdown completed")
	return nil
}

// logFrameStats periodically logs frame statistics
func (s *Server) logFrameStats(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastCount int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentCount := s.frameCount.Load()
			framesSinceLastLog := currentCount - lastCount
			s.logger.Debug("Frame statistics", "total_frames", currentCount, "frames_last_5s", framesSinceLastLog)
			lastCount = currentCount
		}
	}
}

type SessionResponse struct {
	LobbySessionUUID string                       `json:"lobby_session_id"`
	Events           []*SessionEventResponseEntry `json:"events"`
}

// SessionResponseV3 represents the v3 API response format with full schema
type SessionResponseV3 struct {
	LobbySessionUUID string                  `json:"lobby_session_id"`
	Frames           []*SessionFrameDocument `json:"frames"`
	TotalCount       int64                   `json:"total_count"`
}

// SessionEventResponseEntry represents a simple session event object (v1 format)
type SessionEventResponseEntry struct {
	UserID    string          `json:"user_id,omitempty"`
	FrameData json.RawMessage `json:"frame,omitempty"`
}
