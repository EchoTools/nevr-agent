package api

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the API server
type Metrics struct {
	// Frame metrics
	FramesReceived   prometheus.Counter
	FramesProcessed  prometheus.Counter
	FramesWithEvents prometheus.Counter

	// Match metrics
	MatchesActive    prometheus.Gauge
	MatchesCompleted prometheus.Counter
	MatchesByMode    *prometheus.CounterVec

	// Storage metrics
	StorageBytesUsed prometheus.Gauge
	StorageFileCount prometheus.Gauge

	// WebSocket metrics
	WebSocketConnections prometheus.Gauge
	WebSocketMessages    prometheus.Counter

	// API metrics
	APIRequestDuration *prometheus.HistogramVec
	APIRequestsTotal   *prometheus.CounterVec

	// Rate limiting
	RateLimitExceeded prometheus.Counter

	// Player lookup
	PlayerLookups       prometheus.Counter
	PlayerLookupErrors  prometheus.Counter
	PlayerLookupLatency prometheus.Histogram
}

// NewMetrics creates a new Metrics instance with all metrics registered
func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "evrtelemetry"
	}

	return &Metrics{
		FramesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "frames_received_total",
			Help:      "Total number of frames received from clients",
		}),
		FramesProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "frames_processed_total",
			Help:      "Total number of frames processed and stored",
		}),
		FramesWithEvents: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "frames_with_events_total",
			Help:      "Total number of frames containing events",
		}),

		MatchesActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "matches_active",
			Help:      "Number of matches currently being recorded",
		}),
		MatchesCompleted: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "matches_completed_total",
			Help:      "Total number of matches completed",
		}),
		MatchesByMode: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "matches_by_mode_total",
			Help:      "Total number of matches by game mode",
		}, []string{"mode"}),

		StorageBytesUsed: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "storage_bytes_used",
			Help:      "Total bytes used by capture storage",
		}),
		StorageFileCount: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "storage_file_count",
			Help:      "Number of capture files in storage",
		}),

		WebSocketConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "websocket_connections",
			Help:      "Number of active WebSocket connections",
		}),
		WebSocketMessages: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "websocket_messages_total",
			Help:      "Total number of WebSocket messages received",
		}),

		APIRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "api_request_duration_seconds",
			Help:      "Histogram of API request durations",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"method", "path", "status"}),
		APIRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "api_requests_total",
			Help:      "Total number of API requests",
		}, []string{"method", "path", "status"}),

		RateLimitExceeded: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rate_limit_exceeded_total",
			Help:      "Total number of rate limit exceeded events",
		}),

		PlayerLookups: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "player_lookups_total",
			Help:      "Total number of player lookups performed",
		}),
		PlayerLookupErrors: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "player_lookup_errors_total",
			Help:      "Total number of player lookup errors",
		}),
		PlayerLookupLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "player_lookup_duration_seconds",
			Help:      "Histogram of player lookup durations",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5},
		}),
	}
}

// Handler returns the Prometheus HTTP handler
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

// MetricsMiddleware wraps an HTTP handler to record request metrics
func (m *Metrics) MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		status := http.StatusText(wrapped.statusCode)

		m.APIRequestDuration.WithLabelValues(r.Method, r.URL.Path, status).Observe(duration)
		m.APIRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// UpdateStorageMetrics updates storage-related metrics
func (m *Metrics) UpdateStorageMetrics(bytesUsed int64, fileCount, activeMatches int) {
	m.StorageBytesUsed.Set(float64(bytesUsed))
	m.StorageFileCount.Set(float64(fileCount))
	m.MatchesActive.Set(float64(activeMatches))
}

// RecordFrame records metrics for a received frame
func (m *Metrics) RecordFrame(hasEvents bool) {
	m.FramesReceived.Inc()
	m.FramesProcessed.Inc()
	if hasEvents {
		m.FramesWithEvents.Inc()
	}
}

// RecordMatchStart records metrics when a new match starts
func (m *Metrics) RecordMatchStart(mode string) {
	m.MatchesActive.Inc()
	m.MatchesByMode.WithLabelValues(mode).Inc()
}

// RecordMatchEnd records metrics when a match ends
func (m *Metrics) RecordMatchEnd() {
	m.MatchesActive.Dec()
	m.MatchesCompleted.Inc()
}

// RecordWebSocketConnect records a new WebSocket connection
func (m *Metrics) RecordWebSocketConnect() {
	m.WebSocketConnections.Inc()
}

// RecordWebSocketDisconnect records a WebSocket disconnection
func (m *Metrics) RecordWebSocketDisconnect() {
	m.WebSocketConnections.Dec()
}

// RecordWebSocketMessage records a WebSocket message
func (m *Metrics) RecordWebSocketMessage() {
	m.WebSocketMessages.Inc()
}

// RecordRateLimitExceeded records a rate limit exceeded event
func (m *Metrics) RecordRateLimitExceeded() {
	m.RateLimitExceeded.Inc()
}

// RecordPlayerLookup records a player lookup
func (m *Metrics) RecordPlayerLookup(duration time.Duration, err error) {
	m.PlayerLookups.Inc()
	m.PlayerLookupLatency.Observe(duration.Seconds())
	if err != nil {
		m.PlayerLookupErrors.Inc()
	}
}
