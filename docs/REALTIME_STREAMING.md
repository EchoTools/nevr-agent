# Real-time Streaming API

The API server provides a real-time WebSocket streaming API that allows clients to subscribe to live match data with support for seeking and rewinding.

## Overview

The streaming API enables:
- **Live Match Streaming**: Subscribe to active matches and receive frames in real-time
- **Historical Playback**: Seek to any point in a match's history
- **Multi-match Support**: Subscribe to multiple matches simultaneously
- **Frame Buffering**: Recent frames are buffered for instant rewind/seek

## Endpoints

### WebSocket Stream

```
WebSocket: /ws/stream
```

Connect to this endpoint to subscribe to match streams.

### REST Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/matches` | GET | List available matches |
| `/api/matches/{id}` | GET | Get match details |
| `/api/matches/{id}/download` | GET | Download match file |

## WebSocket Protocol

### Message Types

#### Subscribe to Match

```json
{
  "type": "subscribe",
  "match_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

#### Unsubscribe from Match

```json
{
  "type": "unsubscribe",
  "match_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

#### Seek to Frame

```json
{
  "type": "seek",
  "match_id": "550e8400-e29b-41d4-a716-446655440000",
  "frame_index": 1500
}
```

#### Request Frame Range (Historical)

```json
{
  "type": "get_frames",
  "match_id": "550e8400-e29b-41d4-a716-446655440000",
  "start": 0,
  "end": 100
}
```

### Server Messages

#### Frame Data

```json
{
  "type": "frame",
  "match_id": "550e8400-e29b-41d4-a716-446655440000",
  "frame_index": 1501,
  "data": { /* LobbySessionStateFrame */ }
}
```

#### Error Response

```json
{
  "type": "error",
  "error": "Match not found"
}
```

#### Subscription Confirmed

```json
{
  "type": "subscribed",
  "match_id": "550e8400-e29b-41d4-a716-446655440000",
  "frame_count": 5000,
  "is_live": true
}
```

## JavaScript Client Example

```javascript
class MatchStreamClient {
  constructor(serverUrl) {
    this.ws = new WebSocket(`${serverUrl}/ws/stream`);
    this.subscribers = new Map();
    
    this.ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      this.handleMessage(msg);
    };
  }

  subscribe(matchId, onFrame) {
    this.subscribers.set(matchId, onFrame);
    this.ws.send(JSON.stringify({
      type: 'subscribe',
      match_id: matchId
    }));
  }

  unsubscribe(matchId) {
    this.subscribers.delete(matchId);
    this.ws.send(JSON.stringify({
      type: 'unsubscribe',
      match_id: matchId
    }));
  }

  seek(matchId, frameIndex) {
    this.ws.send(JSON.stringify({
      type: 'seek',
      match_id: matchId,
      frame_index: frameIndex
    }));
  }

  handleMessage(msg) {
    if (msg.type === 'frame') {
      const callback = this.subscribers.get(msg.match_id);
      if (callback) {
        callback(msg.data, msg.frame_index);
      }
    }
  }
}

// Usage
const client = new MatchStreamClient('ws://localhost:8081');

client.subscribe('550e8400-e29b-41d4-a716-446655440000', (frame, index) => {
  console.log(`Frame ${index}:`, frame);
  // Update UI with frame data
});

// Seek to frame 1000
client.seek('550e8400-e29b-41d4-a716-446655440000', 1000);
```

## Match Retrieval API

### List Matches

```bash
GET /api/matches?status=completed&limit=10
```

Response:
```json
{
  "matches": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "session_id": "session-123",
      "start_time": "2024-01-15T10:00:00Z",
      "end_time": "2024-01-15T10:15:00Z",
      "frame_count": 9000,
      "file_size": 1234567,
      "status": "completed"
    }
  ]
}
```

### Download Match

```bash
GET /api/matches/{id}/download?format=nevrcap
GET /api/matches/{id}/download?format=echoreplay
```

- `format=nevrcap` (default): Returns the native .nevrcap file
- `format=echoreplay`: Converts and returns as .echoreplay (may be slower)

## Configuration

### Server Configuration

```yaml
apiserver:
  server_address: ":8081"
  
  # Capture storage
  capture_dir: "./captures"
  capture_retention: "168h"      # 7 days
  capture_max_size: 10737418240  # 10GB
```

### Environment Variables

```bash
EVR_APISERVER_CAPTURE_DIR=./captures
EVR_APISERVER_CAPTURE_RETENTION=168h
EVR_APISERVER_CAPTURE_MAX_SIZE=10737418240
```

## Storage Management

The API server automatically manages capture storage:

1. **Retention Policy**: Files older than `capture_retention` are deleted
2. **Size Limit**: When storage exceeds `capture_max_size`, oldest files are removed
3. **Format Priority**: When cleaning up, `.echoreplay` files are deleted before `.nevrcap`

### Monitoring Storage

Check storage metrics via Prometheus (if enabled):

```bash
curl http://localhost:9090/metrics | grep storage
```

Metrics:
- `evr_storage_bytes_used`: Current storage usage in bytes
- `evr_matches_completed_total`: Total completed match recordings

## Prometheus Metrics

Enable metrics with `--metrics-addr :9090`:

| Metric | Type | Description |
|--------|------|-------------|
| `evr_frames_received_total` | Counter | Total frames received |
| `evr_matches_active` | Gauge | Currently active matches |
| `evr_matches_completed_total` | Counter | Completed match recordings |
| `evr_storage_bytes_used` | Gauge | Storage usage in bytes |
| `evr_websocket_connections` | Gauge | Active WebSocket connections |
| `evr_api_request_duration_seconds` | Histogram | API request latency |
| `evr_rate_limit_exceeded_total` | Counter | Rate limit violations |

## Example: Minimap Viewer

See [examples/html/minimap](../examples/) for a complete HTML/JavaScript example that:
- Connects to the WebSocket stream API
- Renders a 2D arena view with player positions
- Displays scores and player jersey numbers
- Supports playback controls (play/pause, seek, rewind)

## Security

### Authentication

The streaming API supports JWT authentication:

```javascript
const ws = new WebSocket('ws://localhost:8081/ws/stream', {
  headers: {
    'Authorization': 'Bearer YOUR_JWT_TOKEN'
  }
});
```

See [WEBSOCKET_STREAM.md](WEBSOCKET_STREAM.md) for JWT configuration details.

### Rate Limiting

The API server enforces rate limits:
- Maximum frame rate: `--max-stream-hz` (default: 60 Hz)
- Connection limits per IP
- Request rate limiting on REST endpoints

## Troubleshooting

### Connection Issues

1. **WebSocket upgrade fails**: Check CORS configuration and firewall rules
2. **Authentication errors**: Verify JWT secret matches between client and server
3. **No frames received**: Ensure match is active and subscription was confirmed

### Performance

1. **High latency**: Reduce frame rate with `--fps` flag on agent
2. **Memory usage**: Adjust frame buffer size or reduce subscribed matches
3. **Storage filling up**: Decrease retention or increase cleanup frequency
