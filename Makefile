# photo project Makefile
# Binaries are written to bin/

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.Commit=$(COMMIT)'

BIN_DIR := bin

.PHONY: all build photod photo clean test vet fmt lint tidy server help

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } \
	/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

all: build ## Build all binaries (default)

build: photod photo ## Build both server and CLI binaries

photod: ## Build the photod server binary → bin/photod
	@mkdir -p $(BIN_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/photod ./cmd/photod
	@echo "built bin/photod ($(VERSION))"

photo: ## Build the photo CLI binary → bin/photo
	@mkdir -p $(BIN_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/photo ./cmd/photo
	@echo "built bin/photo ($(VERSION))"

##@ Run

server: photod ## Build and start the photod server
	./$(BIN_DIR)/photod

##@ Development

test: ## Run all tests
	go test ./...

test-verbose: ## Run all tests with verbose output
	go test -v ./...

test-race: ## Run tests with race detector
	go test -race ./...

cover: ## Run tests and open coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

vet: ## Run go vet
	go vet ./...

fmt: ## Run gofmt on all source files
	gofmt -w -s .

lint: ## Run staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck ./...

tidy: ## Tidy and verify go.mod / go.sum
	go mod tidy
	go mod verify

##@ Cleanup

clean: ## Remove built binaries and coverage output
	rm -rf $(BIN_DIR) coverage.out
	@echo "cleaned"
