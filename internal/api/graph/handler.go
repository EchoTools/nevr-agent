package graph

import (
	"context"
	"encoding/json"
	"net/http"
)

// GraphQLRequest represents a GraphQL request body
type GraphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

// GraphQLResponse represents a GraphQL response body
type GraphQLResponse struct {
	Data   any            `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message    string         `json:"message"`
	Locations  []Location     `json:"locations,omitempty"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Location represents a location in the GraphQL query
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Handler returns an HTTP handler for GraphQL requests
func (r *Resolver) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if req.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
			return
		}

		var gqlReq GraphQLRequest
		if err := json.NewDecoder(req.Body).Decode(&gqlReq); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON request body")
			return
		}

		ctx := req.Context()
		result := r.Execute(ctx, gqlReq)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	})
}

// Execute executes a GraphQL query and returns the response
func (r *Resolver) Execute(ctx context.Context, req GraphQLRequest) *GraphQLResponse {
	// Simple query parser - supports basic queries
	// In production, you would use gqlgen's generated executor

	response := &GraphQLResponse{}

	// Parse and execute the query
	data, err := r.executeQuery(ctx, req)
	if err != nil {
		response.Errors = []GraphQLError{{Message: err.Error()}}
		return response
	}

	response.Data = data
	return response
}

func (r *Resolver) executeQuery(ctx context.Context, req GraphQLRequest) (map[string]any, error) {
	result := make(map[string]any)

	// Simple query routing based on operation name and query content
	// This is a simplified implementation - gqlgen would generate a proper executor

	// Check for health query
	if containsQuery(req.Query, "health") {
		health, err := r.Health(ctx)
		if err != nil {
			return nil, err
		}
		result["health"] = health
	}

	// Check for lobbySession query
	if containsQuery(req.Query, "lobbySession") {
		id, _ := getStringVariable(req.Variables, "id")
		if id != "" {
			session, err := r.LobbySession(ctx, id)
			if err != nil {
				return nil, err
			}
			result["lobbySession"] = session
		}
	}

	// Check for sessionEvents query
	if containsQuery(req.Query, "sessionEvents") {
		lobbySessionID, _ := getStringVariable(req.Variables, "lobbySessionId")
		if lobbySessionID != "" {
			limit := getIntVariable(req.Variables, "limit")
			offset := getIntVariable(req.Variables, "offset")
			events, err := r.SessionEvents(ctx, lobbySessionID, limit, offset)
			if err != nil {
				return nil, err
			}
			result["sessionEvents"] = events
		}
	}

	// Check for storeSessionEvent mutation
	if containsQuery(req.Query, "storeSessionEvent") {
		inputRaw, ok := req.Variables["input"].(map[string]any)
		if ok {
			input := StoreSessionEventInput{
				LobbySessionID: inputRaw["lobbySessionId"].(string),
				FrameData:      inputRaw["frameData"].(map[string]any),
			}
			if userID, ok := inputRaw["userId"].(string); ok {
				input.UserID = &userID
			}
			payload, err := r.StoreSessionEvent(ctx, input)
			if err != nil {
				return nil, err
			}
			result["storeSessionEvent"] = payload
		}
	}

	return result, nil
}

func containsQuery(query, field string) bool {
	// Simple check - in production gqlgen handles this
	return len(query) > 0 && (contains(query, field) || contains(query, "..."))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func getStringVariable(vars map[string]any, key string) (string, bool) {
	if vars == nil {
		return "", false
	}
	if v, ok := vars[key].(string); ok {
		return v, true
	}
	return "", false
}

func getIntVariable(vars map[string]any, key string) *int {
	if vars == nil {
		return nil
	}
	if v, ok := vars[key].(float64); ok {
		i := int(v)
		return &i
	}
	if v, ok := vars[key].(int); ok {
		return &v
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(GraphQLResponse{
		Errors: []GraphQLError{{Message: message}},
	})
}

// PlaygroundHandler returns an HTTP handler that serves the GraphQL Playground
func PlaygroundHandler(endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(playgroundHTML(endpoint)))
	})
}

func playgroundHTML(endpoint string) string {
	return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>EVR Data Recorder - GraphQL Playground</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.26/build/static/css/index.css" />
  <link rel="shortcut icon" href="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.26/build/favicon.png" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.26/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function() {
      GraphQLPlayground.init(document.getElementById('root'), {
        endpoint: '` + endpoint + `',
        settings: {
          'editor.theme': 'dark',
          'editor.cursorShape': 'line',
          'editor.fontSize': 14,
          'editor.fontFamily': "'Source Code Pro', 'Consolas', 'Inconsolata', 'Droid Sans Mono', 'Monaco', monospace",
          'request.credentials': 'same-origin',
        }
      });
    });
  </script>
</body>
</html>`
}
