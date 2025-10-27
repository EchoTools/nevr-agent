VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0)
LDFLAGS = -X main.version=$(VERSION) -s -w

BINARY = datarecorder
BIN_DIR = .

.PHONY: all build build-linux build-windows test bench clean version replayer agent converter apiserver

all: build

version:
	@echo $(VERSION)

build: | $(BIN_DIR)
	@echo "Building $(BINARY) (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) .

agent: | $(BIN_DIR)
	@echo "Building agent (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/agent ./cmd/agent

replayer: | $(BIN_DIR)
	@echo "Building replayer (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/replayer ./cmd/replayer


converter: | $(BIN_DIR)
	@echo "Building converter (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/converter ./cmd/converter

apiserver: | $(BIN_DIR)
	@echo "Building apiserver (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/apiserver ./cmd/apiserver

build-linux: | $(BIN_DIR)
	@echo "Building linux/amd64 $(BINARY) (version=$(VERSION))"
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux .

build-windows: | $(BIN_DIR)
	@echo "Building windows/amd64 $(BINARY) (version=$(VERSION))"
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY).exe .

bench:
	go test -bench=. -benchmem ./...

test:
	go test ./...

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

clean:
	rm -rf $(BIN_DIR)
