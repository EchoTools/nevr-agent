VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0)
LDFLAGS = -X main.version=$(VERSION) -s -w

# Main consolidated binary
BINARY := agent

# OS detection
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Windows-specific variables
WINDOWS_BINARY := $(BINARY).exe

.PHONY: all version build windows linux clean test bench lint install-hooks

all: build

version:
	@echo $(VERSION)

# Install git hooks
install-hooks:
	@echo "Installing git hooks..."
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "Git hooks installed."

# Run linting
lint:
	@echo "Running linters..."
	go fmt ./...
	go vet ./...

# Run smoke tests only
smoke-test:
	@echo "Running smoke tests..."
	go test -v -short -run "^TestCLI" ./cmd/agent/...

# Build the main consolidated binary
build:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH) (version=$(VERSION))"
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent

# Build for Windows
windows:
	@echo "Building $(WINDOWS_BINARY) for windows/amd64 (version=$(VERSION))"
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(WINDOWS_BINARY) ./cmd/agent

# Build for Linux
linux:
	@echo "Building $(BINARY) for linux/amd64 (version=$(VERSION))"
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent

bench:
	go test -bench=. -benchmem ./...

test:
	go test ./...

clean:
	rm -f $(BINARY) $(WINDOWS_BINARY)
