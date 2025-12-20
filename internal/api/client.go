package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Client represents a client for the session events service
type Client struct {
	baseURL    string
	httpClient *http.Client
	jwtToken   string
	userAgent  string
}

// ClientConfig holds configuration for the session events client
type ClientConfig struct {
	BaseURL   string        // Base URL of the session events service (e.g., "http://localhost:8080")
	Timeout   time.Duration // HTTP request timeout (default: 30 seconds)
	JWTToken  string        // JWT token for authentication
	UserAgent string        // User-Agent header value
}

// NewClient creates a new session events client
func NewClient(config ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if config.UserAgent == "" {
		config.UserAgent = "NEVR-Agent"
	}

	return &Client{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		jwtToken:  config.JWTToken,
		userAgent: config.UserAgent,
	}
}

// StoreSessionEventResponse represents the response from storing a session event
type StoreSessionEventResponse struct {
	Success          bool   `json:"success"`
	LobbySessionUUID string `json:"lobby_session_id"`
}

// GetSessionEventsResponse represents the response from retrieving session events
type GetSessionEventsResponse struct {
	LobbySessionUUID string                              `json:"lobby_session_id"`
	Count            int                                 `json:"count"`
	Events           []*telemetry.LobbySessionStateFrame `json:"events"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// StoreSessionEvent stores a session event to the server
func (c *Client) StoreSessionEvent(ctx context.Context, event *telemetry.LobbySessionStateFrame) (*StoreSessionEventResponse, error) {
	// Convert protobuf to JSON
	jsonData, err := protojson.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protobuf to JSON: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/lobby-session-events", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned error: %d %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	// Parse response
	var response StoreSessionEventResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// GetSessionEvents retrieves session events by match ID
func (c *Client) GetSessionEvents(ctx context.Context, lobbySessionUUID string) (*GetSessionEventsResponse, error) {
	if lobbySessionUUID == "" {
		return nil, fmt.Errorf("lobby_session_id is required")
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/lobby-session-events/"+lobbySessionUUID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned error: %d %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	// Parse response
	var response GetSessionEventsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// HealthCheck performs a health check against the server
func (c *Client) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned error: %d %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	// Parse response
	var response HealthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// SetJWTToken updates the JWT token for subsequent requests
func (c *Client) SetJWTToken(token string) {
	c.jwtToken = token
}

// GetJWTToken returns the current JWT token
func (c *Client) GetJWTToken() string {
	return c.jwtToken
}

// NewSessionEventsClient is a convenience function to create a new session events client
func NewSessionEventsClient(baseURL string, jwtToken string) *Client {
	return NewClient(ClientConfig{
		BaseURL:  baseURL,
		JWTToken: jwtToken,
	})
}
