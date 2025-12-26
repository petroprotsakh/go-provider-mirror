.PHONY: build install test fmt lint

PKG         := github.com/petroprotsakh/go-provider-mirror
BINARY_NAME := provider-mirror
BUILD_DIR   := ./build
MAIN_PATH   := ./cmd/provider-mirror

VERSION    ?= $(or $(shell git describe --tags --abbrev=0 2>/dev/null),dev)
COMMIT     ?= $(or $(shell git rev-parse --short HEAD 2>/dev/null),unknown)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildTime=$(BUILD_TIME)

# Default target
all: build

# Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

# Install to $GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" $(MAIN_PATH)

# Run tests
test:
	go test -v -race -cover ./...

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run ./...
