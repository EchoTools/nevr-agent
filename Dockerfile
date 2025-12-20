# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o agent ./cmd/agent

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/agent .

# Expose port
EXPOSE 8080

# Set environment variables with defaults
ENV MONGO_URI=mongodb://localhost:27017
ENV SERVER_ADDRESS=:8080

# Add metadata labels for container registry
LABEL org.opencontainers.image.title="NEVR Agent"
LABEL org.opencontainers.image.description="Recording, streaming and API server for Echo VR telemetry"
LABEL org.opencontainers.image.url="https://github.com/EchoTools/nevr-agent"
LABEL org.opencontainers.image.source="https://github.com/EchoTools/nevr-agent"
LABEL org.opencontainers.image.vendor="EchoTools"

# Run the binary
CMD ["./agent", "serve"]