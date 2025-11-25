VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0)
LDFLAGS = -X main.version=$(VERSION) -s -w

# Binaries under cmd/
CMDS := agent apiserver converter dumpevents replayer webviewer

.PHONY: all version cmds $(CMDS) bench test clean build-%

all: cmds

version:
	@echo $(VERSION)

# Build all cmd/* binaries
cmds: $(CMDS)

# Individual cmd/* targets (phony wrappers)
$(CMDS): %: build-%

# Pattern rule to build a cmd/* binary
build-%:
	@echo "Building $* (version=$(VERSION))"
	go build -ldflags "$(LDFLAGS)" -o $* ./cmd/$*

bench:
	go test -bench=. -benchmem ./...

test:
	go test ./...

clean:
	rm -f $(CMDS)
