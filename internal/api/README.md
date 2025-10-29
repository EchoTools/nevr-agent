# Session Events Package

This package provides a standalone HTTP service for storing and retrieving session events from MongoDB.

## Features

- **HTTP API**: RESTful endpoints for session event management
- **MongoDB Storage**: Persistent storage with automatic indexing
- **Graceful Shutdown**: Context-based shutdown handling
- **Health Checks**: Built-in health monitoring
- **Configurable**: Environment-based configuration

## API Endpoints

### Store Session Event
```
POST /lobby-session-events
```

**Headers:**
- `Content-Type: application/json`
- `X-Node-ID: <node-id>` (optional, defaults to "default-node")
- `X-User-ID: <user-id>` (optional)

**Body:** JSON representation of `rtapi.LobbySessionStateFrame`

**Response:**
```json
{
  "success": true,
  "match_id": "uuid.node"
}
```

### Get Session Events
```
GET /lobby-session-events/{match_id}
```

**Response:**
```json
{
  "match_id": "uuid.node",
  "count": 2,
  "events": [
    {
      "match_id": "uuid.node",
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

### As a Standalone Service

```go
package main

import (
    "context"
    "github.com/heroiclabs/nakama/v3/pkg/sessionevents"
)

func main() {
    config := sessionevents.DefaultConfig()
    config.MongoURI = "mongodb://localhost:27017"
    config.ServerAddress = ":8080"

    service, err := sessionevents.NewService(config, nil)
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := service.Initialize(ctx); err != nil {
        panic(err)
    }

    if err := service.Start(ctx); err != nil {
        panic(err)
    }
}
```

### As a Library

```go
package main

import (
    "context"
    "github.com/heroiclabs/nakama/v3/pkg/sessionevents"
    "go.mongodb.org/mongo-driver/mongo"
)

func main() {
    // Use individual functions
    var mongoClient *mongo.Client // ... initialize
    
    event := &sessionevents.SessionEvent{
        MatchID: sessionevents.MatchID{
            UUID: uuid.New(),
            Node: "node1",
        },
        UserID: "user123",
        Data:   nil, // Your rtapi.LobbySessionStateFrame
    }

    err := sessionevents.StoreSessionEvent(context.Background(), mongoClient, event)
    if err != nil {
        panic(err)
    }
}
```

## Configuration

### Environment Variables

- `MONGO_URI`: MongoDB connection string (default: "mongodb://localhost:27017")
- `SERVER_ADDRESS`: HTTP server bind address (default: ":8080")

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

## Running the Example

```bash
# Set environment variables (optional)
export MONGO_URI="mongodb://localhost:27017"
export SERVER_ADDRESS=":8080"

# Run the example
go run cmd/main.go
```

## Dependencies

- `github.com/echotools/nevr-common/v4/gen/go/rtapi` - Protocol buffer definitions
- `github.com/gofrs/uuid/v5` - UUID generation and parsing
- `github.com/gorilla/mux` - HTTP routing
- `go.mongodb.org/mongo-driver` - MongoDB driver
- `google.golang.org/protobuf` - Protocol buffer support

## Database Schema

The package stores session events in MongoDB with the following structure:

```json
{
  "_id": "ObjectId",
  "match_id": "uuid.node",
  "user_id": "string",
  "data": {
    // rtapi.LobbySessionStateFrame data
  }
}
```

### Indexes

The service automatically creates the following indexes:

1. `{ "match_id": 1 }` - For efficient match-based queries
2. `{ "match_id": 1, "timestamp": 1 }` - For sorted temporal queries