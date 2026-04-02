.PHONY: help up down test test-integration lint build clean tools proto fmt migrate-up migrate-down

# Default target
help:
	@echo "Shisa Services - Available targets:"
	@echo "  make up               - Start all services with docker-compose"
	@echo "  make down             - Stop all services and clean up"
	@echo "  make test             - Run unit tests"
	@echo "  make test-integration - Run integration tests (requires stack running)"
	@echo "  make lint             - Run linters"
	@echo "  make build            - Build all service binaries"
	@echo "  make clean            - Clean build artifacts"
	@echo "  make tools            - Install development tools"
	@echo "  make proto            - Generate protobuf code"
	@echo "  make fmt              - Format code"
	@echo "  make migrate-up       - Run database migrations"
	@echo "  make migrate-down     - Rollback last migration"

# Start the development stack
up:
	@echo "Starting services..."
	docker compose -f deploy/docker-compose.yml up -d --wait
	@echo "Services started and healthy."

# Stop and clean up
down:
	@echo "Stopping services..."
	docker compose -f deploy/docker-compose.yml down -v

# Run unit tests
test:
	@echo "Running unit tests..."
	go test -count=1 ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -count=1 -tags=integration ./...

# Run linters
lint:
	@echo "Running golangci-lint..."
	$(shell go env GOPATH)/bin/golangci-lint run --timeout 5m ./...
	@echo "Running go vet..."
	go vet ./...
	@echo "Checking go mod tidy..."
	go mod tidy
	git diff --exit-code go.mod go.sum

# Build all service binaries
build:
	@echo "Building trading service..."
	go build -o bin/trading ./cmd/trading
	@echo "Building platform service..."
	go build -o bin/platform ./cmd/platform
	@echo "Building settlement worker..."
	go build -o bin/settlement ./cmd/settlement
	@echo "Building indexer..."
	go build -o bin/indexer ./cmd/indexer

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	go clean -cache -testcache

# Install development tools
tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/bufbuild/buf/cmd/buf@latest

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	cd proto && buf generate

# Run database migrations
migrate-up:
	@echo "Running database migrations..."
	go run ./cmd/migrate up

migrate-down:
	@echo "Rolling back database migrations..."
	go run ./cmd/migrate down

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	gofmt -s -w .
	goimports -w .
