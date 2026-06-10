.PHONY: help up down test test-integration lint build clean tools proto fmt migrate-up migrate-down gen-contracts

# Default target
help:
	@echo "Shisa Services - Available targets:"
	@echo "  make up               - Start all services with docker-compose"
	@echo "  make down             - Stop all services and clean up"
	@echo "  make test             - Run unit tests"
	@echo "  make test-integration - Run integration tests (requires stack running)"
	@echo "  make lint             - Run linters"
	@echo "  make build            - Build all service binaries (regenerates contract bindings first)"
	@echo "  make gen-contracts    - Regenerate abigen contract bindings from vendored ABIs"
	@echo "  make clean            - Clean build artifacts (including generated contract bindings)"
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

# Run unit tests (regenerate bindings first so a fresh clone can `make test`).
test: gen-contracts
	@echo "Running unit tests..."
	go test -count=1 ./...

# Run integration tests
test-integration: gen-contracts
	@echo "Running integration tests..."
	go test -count=1 -tags=integration ./...

# Run linters
lint: gen-contracts
	@echo "Running golangci-lint..."
	$(shell go env GOPATH)/bin/golangci-lint run --timeout 5m ./...
	@echo "Running go vet..."
	go vet ./...
	@echo "Checking go mod tidy..."
	go mod tidy
	git diff --exit-code go.mod go.sum

# Build all service binaries. Depends on gen-contracts because the
# shared eth package imports the generated bindings, which are
# .gitignored — a fresh clone needs them regenerated before `go build`
# can find the packages.
build: gen-contracts
	@echo "Building trading service..."
	go build -o bin/trading ./cmd/trading
	@echo "Building platform service..."
	go build -o bin/platform ./cmd/platform
	@echo "Building settlement worker..."
	go build -o bin/settlement ./cmd/settlement
	@echo "Building indexer..."
	go build -o bin/indexer ./cmd/indexer

# Regenerate abigen contract bindings from the vendored ABI JSON under
# internal/shared/eth/abi/. Generated code is .gitignored; refreshing
# the underlying ABI is documented in internal/shared/eth/abi/README.md.
gen-contracts:
	@echo "Generating contract bindings..."
	@command -v abigen > /dev/null 2>&1 || { \
		echo "abigen not on PATH — run 'make tools' first" >&2; exit 1; }
	@mkdir -p internal/shared/eth/gen/conditionaltokens internal/shared/eth/gen/negriskadapter
	go generate ./internal/shared/eth/...

# Clean build artifacts (and the generated contract bindings)
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf internal/shared/eth/gen/
	go clean -cache -testcache

# Install development tools
tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/ethereum/go-ethereum/cmd/abigen@latest

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
