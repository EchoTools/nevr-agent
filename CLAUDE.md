# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

nevr-agent is the polling agent and file converter for the nEVR platform. It polls game API endpoints for telemetry frames, writes them to various outputs (WebSocket, file, stdout), and converts between legacy `.echoreplay` and protobuf-based `.tape`/`.nevrcap` file formats with round-trip validation.

## Build & Test

```bash
make build          # Build binary to bin/agent
make test           # Run all tests (go test ./...)
make lint           # Format and vet (go fmt + go vet)
make build-all      # Cross-compile for current OS, Windows, Linux
make image          # Build Docker image
make clean          # Remove build artifacts
```

Single-test example:
```bash
go test -run TestName ./cmd/agent/
```

## Architecture

```
cmd/agent/          CLI entry point (cobra). Subcommands:
  main.go           Root command setup
  agent.go          Polling agent (connects to game API, streams frames)
  converter.go      File format converter (echoreplay <-> tape/nevrcap)
  replayer.go       Replay recorded sessions
  dumpevents.go     Dump events from capture files
  apiserver.go      Embedded API/metrics server
  migrate.go        Data migration utilities
  version_check.go  Automatic version checking
cmd/validator/      Standalone capture file validator
internal/
  agent/            Core polling agent logic
  amqp/             RabbitMQ message publishing
  api/              HTTP API and GraphQL server
    graph/          GraphQL schema and resolvers
  config/           YAML-based configuration loading
tools/              Benchmark tooling
scripts/            Platform-specific helper scripts
```

## Conventions

- **CLI framework**: cobra (`spf13/cobra`). Each subcommand is a file in `cmd/agent/`.
- **Logging**: `go.uber.org/zap` structured logger.
- **Config**: YAML config files loaded via `internal/config`. See `agent.yaml.example`.
- **Protobuf types**: Imported from `github.com/echotools/nevr-capture/v3` and `nevr-common/v4`. No `.proto` files live in this repo.
- **Error handling**: Return errors up; don't panic. Use `fmt.Errorf` with `%w` for wrapping.
- **Naming**: Standard Go conventions. Package names are single lowercase words.
- **Build tags**: None required. `CGO_ENABLED=0` for all builds.
- **Module path**: `github.com/echotools/nevr-agent/v4`.

## Dependencies

Key external dependencies:

| Package | Purpose |
|---|---|
| `nevr-capture/v3` | Protobuf capture format types and serialization |
| `nevr-common/v4` | Shared platform types and utilities |
| `spf13/cobra` | CLI framework |
| `gorilla/websocket` | WebSocket output for live streaming |
| `go.uber.org/zap` | Structured logging |
| `google.golang.org/protobuf` | Protobuf runtime |
| `rabbitmq/amqp091-go` | AMQP message publishing |
| `prometheus/client_golang` | Metrics exposition |
| `heroiclabs/nakama-common` | Game server API types |
| `go.mongodb.org/mongo-driver` | MongoDB storage |
