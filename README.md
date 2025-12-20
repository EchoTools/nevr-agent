# nevr-agent

nevr-agent is a single CLI binary (`agent`) for recording, converting, and replaying EchoVR game session and player bone data.

## Features

- **Agent**: Record session and player bone data from EchoVR game servers via HTTP API polling
  - Advanced frame filtering (FPS control, game mode filtering, active-only mode)
  - Bone data exclusion to reduce payload size
  - Idle/active FPS switching for bandwidth optimization
- **API Server**: HTTP server for storing and retrieving session event data with MongoDB backend
  - Capture storage management with retention policies and size limits
  - Real-time WebSocket streaming API with seek/rewind support
  - Prometheus metrics endpoint
  - Player lookup integration with caching
- **Converter**: Convert between .echoreplay (zip) and .nevrcap (zstd compressed) file formats
  - Progress bar support for large file conversions
- **Replayer**: HTTP server for replaying recorded session data

## Prerequisites

- Go 1.25 or later (for building from source)
- MongoDB (for API server functionality)

## Installation

### Download Pre-built Binaries

Download the latest release for your platform from the [Releases](https://github.com/EchoTools/nevr-agent/releases) page.

### Build from Source

```bash
# Clone the repository
git clone https://github.com/EchoTools/nevr-agent.git
cd nevr-agent

# Build the consolidated binary
make build

# Or build for specific platforms
make linux    # Build for Linux
make windows  # Build for Windows
```

## Usage

The `agent` application provides a unified CLI with subcommands for different functionality.

```bash
# View available commands
agent --help

# Get help for a specific command
agent stream --help
```

### Agent - Record Game Data

Record session and player bone data from EchoVR game servers:

```bash
# Basic recording from localhost ports 6721-6730 at 30Hz
agent stream --frequency 30 --output ./output 127.0.0.1:6721-6730

# Record with streaming to Nakama server
agent stream --stream --stream-username myuser --stream-password mypass 127.0.0.1:6721

# Record with Events API enabled
agent stream --events --events-url http://localhost:8081 127.0.0.1:6721-6730

# Stream all frames at 30 FPS, excluding bone data for smaller payloads
agent stream --all-frames --fps 30 --exclude-bones 127.0.0.1:6721

# Only stream Echo Arena matches during active gameplay
agent stream --include-modes echo_arena --active-only 127.0.0.1:6721

# Reduce bandwidth with idle FPS (1 FPS in lobby, 30 FPS during gameplay)
agent stream --fps 30 --idle-fps 1 --active-only 127.0.0.1:6721-6730
```

#### Stream Filtering Options

| Flag | Description |
|------|-------------|
| `--all-frames` | Send all frames, not just frames with events |
| `--fps <n>` | Target frames per second (0 = use polling frequency) |
| `--idle-fps <n>` | Frame rate for non-gametime frames (default: 1) |
| `--include-modes` | Only stream these game modes (comma-separated) |
| `--exclude-modes` | Exclude these game modes from streaming |
| `--exclude-bones` | Exclude player bone data to reduce payload size |
| `--active-only` | Only stream frames during active gameplay |
| `--exclude-paused` | Exclude paused frames (with `--active-only`) |

### API Server - Session Events API

Run an HTTP server for storing and retrieving session events:

```bash
# Start with default settings
agent serve

# Custom MongoDB URI and port
agent serve --mongo-uri mongodb://localhost:27017 --server-address :8081

# Enable capture storage with retention (7 days, max 10GB)
agent serve --capture-dir ./captures --capture-retention 168h --capture-max-size 10737418240

# Enable Prometheus metrics on port 9090
agent serve --metrics-addr :9090

# Full production setup
agent serve \
  --mongo-uri mongodb://localhost:27017 \
  --capture-dir ./captures \
  --capture-retention 168h \
  --metrics-addr :9090 \
  --jwt-secret "your-secret-key"
```

#### API Server Features

- **Capture Storage**: Automatically stores match recordings with configurable retention and size limits
- **Match Retrieval**: Download completed matches via REST API with format conversion
- **Real-time Streaming**: WebSocket API for live match data with seek/rewind support
- **Prometheus Metrics**: `/metrics` endpoint for monitoring frames, matches, connections, and storage
- **Player Lookup**: Integration with echovrce API for player information with LRU caching

See [docs/WEBSOCKET_STREAM.md](docs/WEBSOCKET_STREAM.md) for WebSocket API details.

### Converter - Format Conversion

Convert between replay file formats:

```bash
# Auto-detect conversion (echoreplay → nevrcap or vice versa)
agent convert --input game.echoreplay

# Specify output file
agent convert --input game.nevrcap --output converted.echoreplay

# Force specific format
agent convert --input game.echoreplay --format nevrcap

# Show progress bar for large files
agent convert --input large_game.echoreplay --progress
```

### Replayer - Replay Sessions

Replay recorded sessions via HTTP server:

```bash
# Replay a single file
agent replay game.echoreplay

# Replay multiple files in sequence
agent replay game1.echoreplay game2.echoreplay

# Loop playback continuously
agent replay --loop game.echoreplay

# Custom bind address
agent replay --bind 0.0.0.0:8080 game.echoreplay
```

## Configuration

The application supports multiple configuration methods (in order of precedence):

1. **Command-line flags** (highest priority)
2. **Environment variables** (prefix with `EVR_`)
3. **Configuration file** (YAML format)
4. **Default values** (lowest priority)

### Configuration File

Create a `agent.yaml` file in your working directory or specify with `--config`:

```yaml
# Global configuration
debug: false
log_level: info

# Agent configuration
agent:
  frequency: 10
  output_directory: ./output
  stream_enabled: false

# API Server configuration
apiserver:
  server_address: ":8081"
  mongo_uri: mongodb://localhost:27017
```

See [agent.yaml.example](agent.yaml.example) for a complete example.

### Environment Variables

All configuration can be set via environment variables with the `EVR_` prefix:

```bash
# Agent configuration
export EVR_AGENT_FREQUENCY=30
export EVR_AGENT_OUTPUT_DIRECTORY=./recordings

# Stream credentials
export EVR_AGENT_STREAM_USERNAME=myuser
export EVR_AGENT_STREAM_PASSWORD=mypassword

# Run the agent
agent stream 127.0.0.1:6721-6730
```

You can also use a `.env` file. See [.env.example](.env.example) for all available variables.

### Credential Management

Credentials (API keys, passwords, database URIs) can be managed securely:

- **Environment variables**: Set sensitive values as environment variables
- **.env file**: Store credentials in a `.env` file (never commit this file!)
- **Config file**: Use for non-sensitive configuration (can be committed)

## Development

### Building

```bash
# Build for current OS
make build

# Build all legacy individual commands
make legacy

# Run tests
make test

# Run benchmarks
make bench

# Clean build artifacts
make clean
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...
```

## License

See [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
