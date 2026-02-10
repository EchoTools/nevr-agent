# Session Events Package

This package provides HTTP/WebSocket services for storing and retrieving session events from MongoDB.

## Features

- **WebSocket Streaming**: Real-time event streaming via WebSocket (recommended)
- **HTTP API**: RESTful endpoints for reading session events
- **GraphQL API**: Query interface for session data
- **MongoDB Storage**: Persistent storage with automatic indexing
- **Graceful Shutdown**: Context-based shutdown handling
- **Health Checks**: Built-in health monitoring
- **Configurable**: Environment-based configuration

## API Endpoints

### WebSocket Streaming (Write)
```
WS /v3/stream
```

Connect via WebSocket to stream events in real-time. This is the primary method for sending session events.

**Authentication:**
- Include JWT token in query parameter: `?token=<jwt-token>`
- Or use `Authorization: Bearer <token>` header during upgrade

### Get Session Events (Read)
```
GET /lobby-session-events/{lobby_session_id}
```

**Response:**
```json
{
  "lobby_session_id": "session-uuid",
  "count": 2,
  "events": [
    {
      "lobby_session_id": "session-uuid",
      "user_id": "user123",
      "data": { ... }
    }
  ]
}
```

### Health Check
```
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2023-10-24T12:00:00Z"
}
```

## Usage

### Streaming Events via WebSocket

Use the `agent stream` command with `--events-stream` to stream session events:

```bash
# Stream to events API via WebSocket
agent stream --events-stream --events-url http://localhost:8081 127.0.0.1:6721
```

### Reading Events

```go
package main

import (
    "context"
    "github.com/echotools/nevr-agent/v4/internal/api"
)

func main() {
    client := api.NewClient(api.ClientConfig{
        BaseURL:  "http://localhost:8081",
        JWTToken: "your-jwt-token",
    })

    events, err := client.GetSessionEvents(context.Background(), "session-uuid")
    if err != nil {
        panic(err)
    }
    
    // Process events...
}
```

## Configuration

### Environment Variables

- `MONGO_URI`: MongoDB connection string (default: "mongodb://localhost:27017")
- `SERVER_ADDRESS`: HTTP server bind address (default: ":8081")

### Configuration Struct

```go
type Config struct {
    MongoURI       string        `json:"mongo_uri"`
    DatabaseName   string        `json:"database_name"`
    CollectionName string        `json:"collection_name"`
    ServerAddress  string        `json:"server_address"`
    MongoTimeout   time.Duration `json:"mongo_timeout"`
    ServerTimeout  time.Duration `json:"server_timeout"`
}
```

## Dependencies

- `github.com/echotools/nevr-common/v4/gen/go/telemetry/v1` - Protocol buffer definitions
- `github.com/gorilla/websocket` - WebSocket support
- `go.mongodb.org/mongo-driver` - MongoDB driver
- `google.golang.org/protobuf` - Protocol buffer support

## Database Schema

The package stores session events in MongoDB with the following structure:

```json
{
  "_id": "ObjectId",
  "lobby_session_id": "string",
  "user_id": "string",
  "frame": {
    // telemetry.LobbySessionStateFrame data
  },
  "event_types": ["array", "of", "event", "types"],
  "timestamp": "ISODate",
  "created_at": "ISODate",
  "updated_at": "ISODate"
}
```

### Indexes

The service automatically creates the following indexes:

1. `{ "lobby_session_id": 1 }` - For efficient session-based queries
2. `{ "lobby_session_id": 1, "timestamp": 1 }` - For sorted temporal queries
