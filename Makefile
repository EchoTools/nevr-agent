# ============================================================================
# NEVR Agent Makefile
# ============================================================================

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY  := agent
PKG     := ./cmd/agent
LDFLAGS := -s -w -X main.version=$(VERSION)
OUT_DIR := bin

# Docker
IMAGE := ghcr.io/echotools/nevr-agent:$(VERSION)

.PHONY: all build run clean test lint image image-push help

.DEFAULT_GOAL := build

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the agent
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY) $(PKG)

run: build ## Build and run
	./$(OUT_DIR)/$(BINARY)

test: ## Run tests
	go test ./...

lint: ## Format and vet
	go fmt ./...
	go vet ./...

image: ## Build Docker image
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) -t ghcr.io/echotools/nevr-agent:latest .

image-push: image ## Push Docker image
	docker push $(IMAGE)
	docker push ghcr.io/echotools/nevr-agent:latest

clean: ## Clean build artifacts
	rm -rf $(OUT_DIR) $(BINARY)
