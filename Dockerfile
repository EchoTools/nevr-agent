# Build stage
FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ENV VERSION=$VERSION

WORKDIR /go/build/agent

# Copy go mod files first to leverage Docker cache for dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$VERSION" \
    -o /go/build-out/agent \
    ./cmd/agent

# Final stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev

LABEL org.opencontainers.image.title="NEVR Agent"
LABEL org.opencontainers.image.description="Recording, streaming and API server for Echo VR telemetry"
LABEL org.opencontainers.image.url="https://github.com/EchoTools/nevr-agent"
LABEL org.opencontainers.image.source="https://github.com/EchoTools/nevr-agent"
LABEL org.opencontainers.image.vendor="EchoTools"
LABEL org.opencontainers.image.version=$VERSION

WORKDIR /agent

# Copy the binary from builder stage
COPY --from=builder /go/build-out/agent /agent/

# Expose port
EXPOSE 8080

# Set environment variables with defaults
ENV MONGO_URI=mongodb://localhost:27017
ENV SERVER_ADDRESS=:8080

# Run as non-root user (distroless nonroot user is 65532)
USER nonroot:nonroot

# Run the binary
ENTRYPOINT ["/agent/agent"]
