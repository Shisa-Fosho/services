.PHONY: help up down test test-integration test-onchain lint build clean tools proto fmt migrate-up migrate-down gen-contracts

# Default target
help:
	@echo "Shisa Services - Available targets:"
	@echo "  make up               - Start all services with docker-compose"
	@echo "  make down             - Stop all services and clean up"
	@echo "  make test             - Run unit tests"
	@echo "  make test-integration - Run integration tests (requires stack running)"
	@echo "  make test-onchain     - Run on-chain tests against an anvil fork of Polygon (requires Docker)"
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

# On-chain integration tests against a fresh anvil fork of Polygon
# mainnet, where our production contracts live at their deployed
# addresses (docs/testing-onchain.md). The fork MUST be started fresh
# per run: public Polygon RPCs only serve state for ~128 recent blocks
# (~4 min), after which anvil's lazy upstream fetches fail. Override
# ONCHAIN_FORK_RPC with an archive endpoint (and optionally set
# ONCHAIN_FORK_BLOCK) for pinned, non-time-sensitive runs.
ONCHAIN_PORT ?= 8546
ONCHAIN_FORK_RPC ?= https://polygon-bor-rpc.publicnode.com
ONCHAIN_CONTAINER = shisa-anvil-onchain

test-onchain: gen-contracts
	@echo "Starting anvil fork of Polygon on port $(ONCHAIN_PORT)..."
	@docker rm -f $(ONCHAIN_CONTAINER) >/dev/null 2>&1 || true
	@docker run -d --name $(ONCHAIN_CONTAINER) -p $(ONCHAIN_PORT):8545 \
		ghcr.io/foundry-rs/foundry:stable \
		"anvil --host 0.0.0.0 --fork-url $(ONCHAIN_FORK_RPC) $${ONCHAIN_FORK_BLOCK:+--fork-block-number $$ONCHAIN_FORK_BLOCK}" \
		>/dev/null
	@attempts=0; \
	until docker exec $(ONCHAIN_CONTAINER) cast block-number --rpc-url http://localhost:8545 >/dev/null 2>&1; do \
		attempts=$$((attempts + 1)); \
		if [ $$attempts -ge 30 ]; then \
			echo "anvil fork failed to become ready" >&2; \
			docker logs $(ONCHAIN_CONTAINER) | tail -20 >&2; \
			docker rm -f $(ONCHAIN_CONTAINER) >/dev/null 2>&1; \
			exit 1; \
		fi; \
		sleep 2; \
	done
	@echo "Fork ready. Running on-chain tests..."
	@ONCHAIN_RPC_URL=http://127.0.0.1:$(ONCHAIN_PORT) go test -count=1 -tags=onchain -run TestOnchain ./internal/platform/market/ -v; \
	status=$$?; \
	docker rm -f $(ONCHAIN_CONTAINER) >/dev/null 2>&1; \
	exit $$status

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
#
# Tool versions are PINNED — never use @latest (see docs/rules/conventions.md
# "Dependency Pinning"). Bump a version here deliberately, in its own commit,
# after verifying the new version builds and lints clean.
#
# GOTOOLCHAIN is forced to a FLOOR of the repo's Go version (the `+auto`
# suffix means "at least this version, upgrade if a tool's go.mod needs more").
# This guarantees tools that embed the Go typechecker — golangci-lint above
# all — are never built with an OLDER toolchain than the repo targets, which
# is what made golangci-lint unable to typecheck our 1.25 code when it silently
# fell back to the base 1.24.4 toolchain. The `+auto` (not a hard pin) still
# lets tools like buf, whose go.mod requires a newer patch release, upgrade.
GO_TOOLCHAIN              := $(shell go env GOVERSION)+auto
GOLANGCI_LINT_VERSION     := v1.64.8
GOIMPORTS_VERSION         := v0.46.0
PROTOC_GEN_GO_VERSION     := v1.36.11
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2
BUF_VERSION               := v1.70.0
ABIGEN_VERSION            := v1.17.2

tools:
	@echo "Installing development tools (pinned versions, built with $(GO_TOOLCHAIN))..."
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/ethereum/go-ethereum/cmd/abigen@$(ABIGEN_VERSION)

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
