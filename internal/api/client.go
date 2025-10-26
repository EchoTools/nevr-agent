package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/echotools/nevr-common/gen/go/rtapi"
	"google.golang.org/protobuf/encoding/protojson"
)

// Client represents a client for the session events service
type Client struct {
	baseURL    string
	httpClient *http.Client
	userID     string
	nodeID     string
}

// ClientConfig holds configuration for the session events client
type ClientConfig struct {
	BaseURL string        // Base URL of the session events service (e.g., "http://localhost:8080")
	Timeout time.Duration // HTTP request timeout (default: 30 seconds)
	UserID  string        // User ID to include in requests
	NodeID  string        // Node ID to include in requests
}

// NewClient creates a new session events client
func NewClient(config ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if config.NodeID == "" {
		config.NodeID = "default-node"
	}

	return &Client{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		userID: config.UserID,
		nodeID: config.NodeID,
	}
}

// StoreSessionEventResponse represents the response from storing a session event
type StoreSessionEventResponse struct {
	Success bool   `json:"success"`
	MatchID string `json:"match_id"`
}

// GetSessionEventsResponse represents the response from retrieving session events
type GetSessionEventsResponse struct {
	MatchID string                          `json:"match_id"`
	Count   int                             `json:"count"`
	Events  []*rtapi.LobbySessionStateFrame `json:"events"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// StoreSessionEvent stores a session event to the server
func (c *Client) StoreSessionEvent(ctx context.Context, event *rtapi.LobbySessionStateFrame) (*StoreSessionEventResponse, error) {
	// Convert protobuf to JSON
	jsonData, err := protojson.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protobuf to JSON: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session-events", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if c.userID != "" {
		req.Header.Set("X-User-ID", c.userID)
	}
	if c.nodeID != "" {
		req.Header.Set("X-Node-ID", c.nodeID)
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
func (c *Client) GetSessionEvents(ctx context.Context, matchID string) (*GetSessionEventsResponse, error) {
	if matchID == "" {
		return nil, fmt.Errorf("match_id is required")
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session-events/"+matchID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	if c.userID != "" {
		req.Header.Set("X-User-ID", c.userID)
	}
	if c.nodeID != "" {
		req.Header.Set("X-Node-ID", c.nodeID)
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

// SetUserID updates the user ID for subsequent requests
func (c *Client) SetUserID(userID string) {
	c.userID = userID
}

// SetNodeID updates the node ID for subsequent requests
func (c *Client) SetNodeID(nodeID string) {
	c.nodeID = nodeID
}

// GetUserID returns the current user ID
func (c *Client) GetUserID() string {
	return c.userID
}

// GetNodeID returns the current node ID
func (c *Client) GetNodeID() string {
	return c.nodeID
}

// NewSessionEventsClient is a convenience function to create a new session events client
func NewSessionEventsClient(baseURL string, userID string, nodeID string) *Client {
	return NewClient(ClientConfig{
		BaseURL: baseURL,
		UserID:  userID,
		NodeID:  nodeID,
	})
}
