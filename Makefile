# Makefile for nexus3-go

# Variables
BINARY_NAME=nexus3-go
MAIN_PATH=./cmd/nexus3-go
BUILD_DIR=./bin
DIST_DIR=./dist
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(shell git rev-parse HEAD 2>/dev/null || echo none)
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Build flags
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_TIME)"
BUILD_FLAGS=-trimpath $(LDFLAGS)

.PHONY: all build build-all clean deps tidy fmt lint vuln test test-coverage release help

# Default target
all: clean deps fmt lint test build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Cross-compile for linux/darwin, amd64/arm64
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)_linux_amd64 $(MAIN_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)_linux_arm64 $(MAIN_PATH)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)_darwin_amd64 $(MAIN_PATH)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)_darwin_arm64 $(MAIN_PATH)
	@echo "Build complete: $(DIST_DIR)/"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR) $(DIST_DIR) coverage.out coverage.html

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download

# Tidy go.mod/go.sum
tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy

# Scan for known vulnerabilities
vuln:
	@echo "Running govulncheck..."
	$(GOCMD) run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code
lint:
	@echo "Linting code..."
	$(GOLINT) run

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -race -count=1 ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -race -count=1 -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Create a release with goreleaser
release:
	@echo "Creating release with goreleaser..."
	goreleaser release --clean

# Help
help:
	@echo "Available targets:"
	@echo "  all            - Clean, deps, fmt, lint, test, and build"
	@echo "  build          - Build the application"
	@echo "  build-all      - Cross-build for linux/darwin amd64/arm64 into dist/"
	@echo "  clean          - Clean build artifacts"
	@echo "  deps           - Download dependencies"
	@echo "  tidy           - Run go mod tidy"
	@echo "  fmt            - Format code"
	@echo "  lint           - Lint code"
	@echo "  vuln           - Scan dependencies with govulncheck"
	@echo "  test           - Run tests with the race detector"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  release        - Create a release with goreleaser"
	@echo "  help           - Show this help"
