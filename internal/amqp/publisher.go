package amqp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqplib "github.com/rabbitmq/amqp091-go"
)

const (
	// DefaultQueueName is the default queue name for match events
	DefaultQueueName = "match.events"

	// DefaultReconnectDelay is the delay between reconnection attempts
	DefaultReconnectDelay = 5 * time.Second

	// DefaultPublishTimeout is the default timeout for publishing messages
	DefaultPublishTimeout = 5 * time.Second
)

// MatchEvent represents a match event message published to AMQP
type MatchEvent struct {
	Type           string    `json:"type"`
	LobbySessionID string    `json:"lobby_session_id"`
	UserID         string    `json:"user_id,omitempty"`
	FrameIndex     int       `json:"frame_index,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	PublishedAt    time.Time `json:"published_at"`
}

// Publisher handles publishing messages to RabbitMQ
type Publisher struct {
	uri            string
	queueName      string
	conn           *amqplib.Connection
	channel        *amqplib.Channel
	mu             sync.RWMutex
	closed         bool
	logger         Logger
	reconnectDelay time.Duration
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

func (l *DefaultLogger) Debug(msg string, fields ...any) {}
func (l *DefaultLogger) Info(msg string, fields ...any)  {}
func (l *DefaultLogger) Error(msg string, fields ...any) {}
func (l *DefaultLogger) Warn(msg string, fields ...any)  {}

// Config holds the configuration for the AMQP publisher
type Config struct {
	URI            string
	QueueName      string
	ReconnectDelay time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		URI:            "amqp://guest:guest@localhost:5672/",
		QueueName:      DefaultQueueName,
		ReconnectDelay: DefaultReconnectDelay,
	}
}

// NewPublisher creates a new AMQP publisher
func NewPublisher(config *Config, logger Logger) (*Publisher, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = &DefaultLogger{}
	}

	p := &Publisher{
		uri:            config.URI,
		queueName:      config.QueueName,
		logger:         logger,
		reconnectDelay: config.ReconnectDelay,
	}

	return p, nil
}

// Connect establishes a connection to RabbitMQ
func (p *Publisher) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return fmt.Errorf("publisher is closed")
	}

	conn, err := amqplib.Dial(p.uri)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	p.conn = conn

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}
	p.channel = channel

	// Declare the queue (flat queue approach - no exchange routing)
	_, err = channel.QueueDeclare(
		p.queueName, // name
		true,        // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	p.logger.Info("Connected to RabbitMQ", "uri", p.uri, "queue", p.queueName)
	return nil
}

// Publish publishes a match event to the queue
func (p *Publisher) Publish(ctx context.Context, event *MatchEvent) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("publisher is closed")
	}

	if p.channel == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Set published timestamp
	event.PublishedAt = time.Now().UTC()

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create a context with timeout for publishing
	publishCtx, cancel := context.WithTimeout(ctx, DefaultPublishTimeout)
	defer cancel()

	err = p.channel.PublishWithContext(
		publishCtx,
		"",          // exchange (empty for default exchange)
		p.queueName, // routing key (queue name)
		false,       // mandatory
		false,       // immediate
		amqplib.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqplib.Persistent,
			Timestamp:    event.PublishedAt,
			MessageId:    fmt.Sprintf("%s-%d", event.LobbySessionID, event.Timestamp.UnixNano()),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	p.logger.Debug("Published match event",
		"type", event.Type,
		"lobby_session_id", event.LobbySessionID,
	)

	return nil
}

// PublishSessionEvent is a convenience method to publish a session event
func (p *Publisher) PublishSessionEvent(ctx context.Context, lobbySessionID, userID string, frameIndex int, timestamp time.Time) error {
	event := &MatchEvent{
		Type:           "session.frame",
		LobbySessionID: lobbySessionID,
		UserID:         userID,
		FrameIndex:     frameIndex,
		Timestamp:      timestamp,
	}
	return p.Publish(ctx, event)
}

// Close closes the AMQP connection
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	var errs []error

	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close channel: %w", err))
		}
		p.channel = nil
	}

	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close connection: %w", err))
		}
		p.conn = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing publisher: %v", errs)
	}

	p.logger.Info("AMQP publisher closed")
	return nil
}

// IsConnected returns true if the publisher is connected
func (p *Publisher) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn != nil && !p.conn.IsClosed() && p.channel != nil
}
