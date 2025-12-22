# Build stage
FROM golang:1.25-alpine AS builder

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



FROM debian:bookworm-slim

LABEL org.opencontainers.image.authors="andrew@sprock.io"

ARG version

LABEL version=$version
LABEL variant=agent
LABEL description="Distributed server for social and realtime games and apps."

RUN mkdir -p /agent/data/modules && \
    apt-get update && \
    apt-get -y upgrade && \
    apt-get install -y --no-install-recommends ca-certificates tzdata iproute2 tini && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /agent/
COPY --from=builder "/go/build-out/agent" /agent/
EXPOSE 8080

ENTRYPOINT ["tini", "--", "/agent/agent"]

