# GitHub Copilot Instructions for nevr-agent

## Project Overview

nevr-agent is a unified Go CLI (`agent`) for recording, converting, and replaying EchoVR game session telemetry. Subcommands: `stream`, `serve`, `convert`, `replay`.

## Architecture

```
cmd/agent/           # Cobra CLI commands (main.go, stream.go, etc.)
internal/agent/      # Core writers/pollers implementing FrameWriter interface
internal/api/        # HTTP/WebSocket API server (MongoDB backend, Prometheus metrics)
internal/config/     # Viper-based config with yaml/env/flags hierarchy
internal/amqp/       # RabbitMQ integration
```

**Cross-repo dependencies** (via go.work):
- `nevr-common` → Protobuf definitions (`telemetry.LobbySessionStateFrame`)
- `nevrcap` → Codec implementations (.echoreplay, .nevrcap formats)

## Key Patterns

### CLI Commands (Cobra)
Use local flag variables, NOT `viper.BindPFlags()` - prevents conflicts between subcommands:
```go
func newMyCommand() *cobra.Command {
    var myFlag string  // LOCAL variable
    cmd := &cobra.Command{
        Use: "mycommand [flags] <arg>",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runMyCommand(myFlag, args)
        },
    }
    cmd.Flags().StringVar(&myFlag, "flag", "default", "description")
    return cmd
}
```

### FrameWriter Interface
All output destinations implement this (file writers, stream writers, API writers):
```go
type FrameWriter interface {
    WriteFrame(*telemetry.LobbySessionStateFrame) error
    Close()
    IsStopped() bool
}
```

### Configuration Hierarchy
CLI flags > env vars (`EVR_` prefix) > config file > defaults. See `internal/config/config.go`.

### Logging
Use zap structured logging: `logger.Info("msg", zap.String("key", val), zap.Error(err))`

## Build & Test

```bash
make build         # Build agent binary (version from git tags)
make test          # Run unit tests
make smoke-test    # CLI integration tests
make lint          # gofmt + go vet
make install-hooks # Pre-commit hook (lint + tests)
```

## Common Tasks

**Add subcommand**: Create `cmd/agent/mycommand.go`, add to `rootCmd.AddCommand()` in main.go
**Add writer**: Implement `FrameWriter` in `internal/agent/writer_mytype.go`
**Add config field**: Update struct in `internal/config/config.go` with `yaml:` and `mapstructure:` tags

## Commit Strategy

Break changes into small, focused commits (config → logic → CLI → tests → docs). PRs are squash-merged.

## Error Handling

Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`. Log at handling point only.
