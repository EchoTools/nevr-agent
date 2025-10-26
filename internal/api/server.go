package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/echotools/nevr-common/gen/go/rtapi"
	"github.com/gofrs/uuid/v5"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/protobuf/encoding/protojson"
)

// Server represents the HTTP server for session events
type Server struct {
	mongoClient *mongo.Client
	router      *mux.Router
	logger      Logger
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

// NewServer creates a new session events HTTP server
func NewServer(mongoClient *mongo.Client, logger Logger) *Server {
	if logger == nil {
		logger = &DefaultLogger{}
	}

	s := &Server{
		mongoClient: mongoClient,
		router:      mux.NewRouter(),
		logger:      logger,
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures the HTTP routes
func (s *Server) setupRoutes() {
	s.router.HandleFunc("/session-events", s.storeSessionEventHandler).Methods("POST")
	s.router.HandleFunc("/session-events/{match_id}", s.getSessionEventsHandler).Methods("GET")
	s.router.HandleFunc("/health", s.healthHandler).Methods("GET")
}

// storeSessionEventHandler handles POST requests to store session events
func (s *Server) storeSessionEventHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.Error("Failed to decode request body", "error", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Parse the payload as LobbySessionStateFrame
	msg := &rtapi.LobbySessionStateFrame{}
	if err := protojson.Unmarshal(payload, msg); err != nil {
		s.logger.Error("Failed to unmarshal protobuf payload", "error", err)
		http.Error(w, "Invalid protobuf payload", http.StatusBadRequest)
		return
	}

	// Extract node from request headers or use a default value
	node := r.Header.Get("X-Node-ID")
	if node == "" {
		node = "default-node" // You might want to configure this
	}

	// Extract user ID from request headers
	userID := r.Header.Get("X-User-ID")

	matchID := MatchID{
		UUID: uuid.FromStringOrNil(msg.GetSession().GetSessionId()),
		Node: node,
	}

	if !matchID.IsValid() {
		s.logger.Error("Invalid match ID", "session_id", msg.GetSession().GetSessionId(), "node", node)
		http.Error(w, "Invalid match ID in payload", http.StatusBadRequest)
		return
	}

	event := &SessionEvent{
		MatchID: matchID,
		UserID:  userID,
		Data:    msg,
	}

	// Store the event to MongoDB
	if err := StoreSessionEvent(ctx, s.mongoClient, event); err != nil {
		s.logger.Error("Failed to store session event", "error", err, "match_id", event.MatchID)
		http.Error(w, "Failed to store session event", http.StatusInternalServerError)
		return
	}

	// Return success response
	response := map[string]any{
		"success":  true,
		"match_id": event.MatchID.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug("Stored session event", "match_id", event.MatchID)
}

// getSessionEventsHandler handles GET requests to retrieve session events by match ID
func (s *Server) getSessionEventsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	matchIDStr := vars["match_id"]

	if matchIDStr == "" {
		http.Error(w, "match_id is required", http.StatusBadRequest)
		return
	}

	// Retrieve events from MongoDB
	events, err := RetrieveSessionEventsByMatchID(ctx, s.mongoClient, matchIDStr)
	if err != nil {
		s.logger.Error("Failed to retrieve session events", "error", err, "match_id", matchIDStr)
		http.Error(w, "Failed to retrieve session events", http.StatusInternalServerError)
		return
	}

	// Return response
	response := map[string]any{
		"match_id": matchIDStr,
		"count":    len(events),
		"events":   events,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug("Retrieved session events", "match_id", matchIDStr, "count", len(events))
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

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
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
