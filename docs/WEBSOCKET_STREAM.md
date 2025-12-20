# WebSocket Stream API

The EVR Data Recorder API server provides a WebSocket endpoint for streaming telemetry session events in real-time.

## Overview

The WebSocket stream endpoint allows clients to send session event data over a persistent WebSocket connection with JWT authentication. This is useful for applications that need to stream continuous telemetry data without the overhead of establishing new HTTP connections for each event.

## Endpoint

```
WebSocket: /v3/stream
```

## Authentication

The WebSocket endpoint requires JWT authentication via the `Authorization` header during the initial WebSocket handshake.

### JWT Token Format

The token must be provided as a Bearer token in the Authorization header:

```
Authorization: Bearer <your-jwt-token>
```

### Configuring the JWT Secret

The API server must be configured with a JWT secret key for token validation. This can be set in three ways:

1. **Configuration File** (agent.yaml):
```yaml
apiserver:
  jwt_secret: "your-secret-key-here"
```

2. **Command-line Flag**:
```bash
agent serve --jwt-secret "your-secret-key-here"
```

3. **Environment Variable**:
```bash
export NEVR_APISERVER_JWT_SECRET="your-secret-key-here"
agent serve
```

## Connection

### Establishing a Connection

Connect to the WebSocket endpoint with the JWT token in the Authorization header:

```javascript
const ws = new WebSocket('ws://localhost:8081/v3/stream', {
  headers: {
    'Authorization': 'Bearer YOUR_JWT_TOKEN',
    'X-Node-ID': 'optional-node-id',      // Optional
    'X-User-ID': 'optional-user-id'       // Optional
  }
});
```

### Connection Lifecycle

1. **Handshake**: The server validates the JWT token during the WebSocket upgrade
2. **Active**: Connection is established and ready to receive messages
3. **Ping/Pong**: Server sends periodic pings to keep the connection alive
4. **Close**: Connection closes on error or when client/server disconnects

## Sending Events

Once connected, send session event data as JSON messages. Each message should be a `LobbySessionStateFrame` protobuf message serialized as JSON.

### Message Format

```json
{
  "session": {
    "session_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  // ... additional frame data
}
```

### Example (JavaScript)

```javascript
const ws = new WebSocket('ws://localhost:8081/v3/stream', {
  headers: {
    'Authorization': 'Bearer YOUR_JWT_TOKEN'
  }
});

ws.onopen = () => {
  console.log('Connected to stream');
  
  // Send a session event
  const event = {
    session: {
      session_id: '550e8400-e29b-41d4-a716-446655440000'
    },
    // ... additional event data
  };
  
  ws.send(JSON.stringify(event));
};

ws.onmessage = (event) => {
  const response = JSON.parse(event.data);
  if (response.success) {
    console.log('Event acknowledged');
  } else {
    console.error('Error:', response.error);
  }
};

ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  console.log('Connection closed');
};
```

### Example (Python)

```python
import websocket
import json
import jwt
from datetime import datetime, timedelta

# Generate JWT token (example)
secret = 'your-secret-key-here'
token = jwt.encode(
    {'exp': datetime.utcnow() + timedelta(hours=1)},
    secret,
    algorithm='HS256'
)

# Connect to WebSocket
ws = websocket.WebSocketApp(
    'ws://localhost:8081/v3/stream',
    header={
        'Authorization': f'Bearer {token}'
    },
    on_message=lambda ws, msg: print(f'Received: {msg}'),
    on_error=lambda ws, err: print(f'Error: {err}'),
    on_close=lambda ws, close_status_code, close_msg: print('Connection closed')
)

def on_open(ws):
    print('Connected')
    # Send event
    event = {
        'session': {
            'session_id': '550e8400-e29b-41d4-a716-446655440000'
        }
    }
    ws.send(json.dumps(event))

ws.on_open = on_open
ws.run_forever()
```

## Response Format

The server sends JSON responses for each message received:

### Success Response

```json
{
  "success": true
}
```

### Error Response

```json
{
  "success": false,
  "error": "error description"
}
```

## Error Handling

### Authentication Errors

- **401 Unauthorized**: Missing or invalid JWT token
  - No Authorization header
  - Invalid token format
  - Token signature verification failed
  - Token expired

### Connection Errors

- **400 Bad Request**: Invalid message format
  - Malformed JSON
  - Invalid protobuf structure
  - Missing required fields

- **500 Internal Server Error**: Server-side error
  - Database connection failure
  - Storage error

### Timeout Errors

- Connection will close if no pong response is received within 60 seconds
- Messages must be written within 10 seconds

## Configuration

### Server Configuration

```yaml
apiserver:
  server_address: ":8081"
  mongo_uri: "mongodb://localhost:27017"
  jwt_secret: "your-secret-key-here"
```

### Timeouts

- **Write Timeout**: 10 seconds
- **Read Timeout**: 60 seconds (pong wait)
- **Ping Period**: 54 seconds
- **Max Message Size**: 10 MB

## Security Best Practices

1. **Use Strong JWT Secrets**: Generate a strong, random secret key (at least 32 characters)
2. **Set Token Expiration**: Include `exp` claim in JWT tokens
3. **Use TLS/WSS**: Always use `wss://` (WebSocket Secure) in production
4. **Rotate Keys**: Regularly rotate JWT secret keys
5. **Validate Origins**: Configure CORS settings appropriately via `EVR_APISERVER_CORS_ORIGINS` environment variable

## JWT Token Generation

### Example: Generate a JWT Token

Here's how to generate a valid JWT token for testing:

**Using Python:**
```python
import jwt
from datetime import datetime, timedelta

secret = 'your-secret-key-here'
payload = {
    'exp': datetime.utcnow() + timedelta(hours=1),
    'iat': datetime.utcnow(),
    'sub': 'user-id'  # Optional: user identifier
}

token = jwt.encode(payload, secret, algorithm='HS256')
print(token)
```

**Using Node.js:**
```javascript
const jwt = require('jsonwebtoken');

const secret = 'your-secret-key-here';
const payload = {
  exp: Math.floor(Date.now() / 1000) + (60 * 60),  // 1 hour
  iat: Math.floor(Date.now() / 1000),
  sub: 'user-id'  // Optional: user identifier
};

const token = jwt.sign(payload, secret, { algorithm: 'HS256' });
console.log(token);
```

**Using Go:**
```go
package main

import (
    "fmt"
    "time"
    "github.com/golang-jwt/jwt/v5"
)

func main() {
    secret := []byte("your-secret-key-here")
    
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
        "exp": time.Now().Add(time.Hour * 1).Unix(),
        "iat": time.Now().Unix(),
        "sub": "user-id",
    })
    
    tokenString, err := token.SignedString(secret)
    if err != nil {
        panic(err)
    }
    
    fmt.Println(tokenString)
}
```

## Monitoring

The WebSocket stream endpoint logs the following events:

- **Connection established**: When a client successfully connects
- **Message processed**: When an event is successfully stored
- **Connection closed**: When a client disconnects
- **Errors**: Authentication failures, parsing errors, storage failures

Check the server logs for detailed information about WebSocket connections and events.
