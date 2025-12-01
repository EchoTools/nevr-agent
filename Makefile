VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0)
LDFLAGS = -X main.version=$(VERSION) -s -w

# Binaries under cmd/
CMDS := agent apiserver converter dumpevents replayer webviewer

# OS detection
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Windows-specific variables
WINDOWS_CMDS := $(addsuffix .exe,$(CMDS))

.PHONY: all version cmds windows linux $(CMDS) $(WINDOWS_CMDS) bench test clean build-% build-%-windows

all: cmds

version:
	@echo $(VERSION)

# Build all cmd/* binaries for current OS
cmds: $(CMDS)

# Build all cmd/* binaries for Windows
windows: $(WINDOWS_CMDS)

# Build all cmd/* binaries for Linux
linux: GOOS=linux
linux: $(CMDS)

# Individual cmd/* targets (phony wrappers)
$(CMDS): %: build-%

# Individual Windows targets
$(WINDOWS_CMDS): %.exe: build-%-windows

# Pattern rule to build a cmd/* binary for current/specified OS
build-%:
	@echo "Building $* for $(GOOS)/$(GOARCH) (version=$(VERSION))"
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $* ./cmd/$*

# Pattern rule to build a cmd/* binary for Windows
build-%-windows:
	@echo "Building $*.exe for windows/amd64 (version=$(VERSION))"
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $*.exe ./cmd/$*

bench:
	go test -bench=. -benchmem ./...

test:
	go test ./...

clean:
	rm -f $(CMDS) $(WINDOWS_CMDS)
