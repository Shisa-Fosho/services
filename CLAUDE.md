# CLAUDE.md — Shisa Services

## Project Overview

Prediction market platform (Polymarket fork) — all Go backend services, shared packages, proto definitions, and infrastructure configs. Off-chain CLOB matching with on-chain Polygon settlement. USDC collateral.

## Architecture

```
┌─────────────────────────────────────────────┐
│              External Clients               │
│         (Web App, Bots, Admin App)          │
└──────────┬──────────────────┬───────────────┘
           │ REST + WebSocket │ REST
           ▼                  ▼
┌─────────────────┐  ┌─────────────────┐
│ Trading Service │  │ Platform Service│
│ :8080 (HTTP)    │  │ :8081 (HTTP)    │
│ :9001 (gRPC)    │  │ :9002 (gRPC)    │
│ (metrics :9091) │  │ (metrics :9092) │
│                 │  │                 │
│ • CLOB Engine   │  │ • Market API    │
│ • REST API      │  │ • Data API      │
│ • WebSocket     │  │ • Admin API     │
│ • Auth          │  │ • Affiliate     │
└────────┬────────┘  └────────┬────────┘
         │ gRPC               │
         │    ┌───────────────┘
         ▼    ▼
    ┌──────────────┐
    │ NATS JetStream│
    └──────┬───────┘
           │
    ┌──────┴──────┐
    ▼             ▼
┌────────┐ ┌────────────┐
│Settle- │ │ Indexer    │
│ment    │ │ :9004      │
│Worker  │ │(metrics    │
│:9003   │ │ :9094)     │
│(metrics│ │            │
│ :9093) │ │ All on-    │
│        │ │ chain event│
│On-chain│ │ monitoring │
│settle  │ └────────────┘
└────────┘
         │
         ▼
    [Polygon RPC]
```

## Services

| Service | Responsibility | HTTP Port | gRPC Port | Metrics Port |
|---------|---------------|-----------|-----------|--------------|
| Trading | CLOB engine, REST API, WebSocket, Auth | 8080 | 9001 | 9091 |
| Platform | Market API, Data API, Admin API, Affiliate | 8081 | 9002 | 9092 |
| Settlement Worker | On-chain trade settlement, relayer | — | 9003 | 9093 |
| Indexer | On-chain event monitoring, deposits | — | 9004 | 9094 |

> **Resolution Worker deferred.** Manual resolution handled by admin wallet from frontend + backend verification. Automated resolution (Chainlink, API feeds) planned for future phase.

## Tech Stack

- Go 1.24, gRPC, Protocol Buffers (Buf)
- PostgreSQL 16, NATS JetStream
- OpenTelemetry (tracing), zap (logging), Prometheus (metrics)
- Grafana + Loki (logs) + Tempo (traces)
- golangci-lint, Docker Compose, Foundry (for local Anvil)

## Essential Commands

```bash
make up              # Start full local stack (services + infra)
make down            # Stop and clean up
make build           # Compile all services
make test            # Run unit tests
make test-integration # Run integration tests (requires stack running)
make lint            # Run golangci-lint + go vet
make proto           # Generate protobuf code (buf generate)
make fmt             # Format code (gofmt + goimports)
make migrate-up      # Run database migrations
make migrate-down    # Rollback last migration
make tools           # Install dev tools
```

## Code Organization

```
cmd/                        # Service entry points (main.go per service)
  ├── trading/
  ├── platform/
  ├── settlement/
  └── indexer/
internal/
  ├── platform/             # Shared infrastructure packages
  │   ├── observability/    # Logger, metrics, tracing, context utilities
  │   ├── grpc/             # gRPC server/client helpers, interceptors
  │   ├── nats/             # NATS client, JetStream helpers, instrumentation
  │   ├── postgres/         # Connection pooling, migration helpers
  │   └── auth/             # JWT, HMAC, SIWE verification
  ├── trading/              # Trading service domain
  │   ├── engine/           # CLOB matching engine (in-memory order book)
  │   ├── api/              # REST handlers
  │   ├── ws/               # WebSocket server
  │   └── domain.go         # Order, Trade, Book types
  ├── market/               # Platform service — market domain
  ├── data/                 # Platform service — user data domain
  ├── admin/                # Platform service — admin domain
  ├── affiliate/            # Platform service — referral system
  ├── settlement/           # Settlement worker domain
  └── indexer/              # Indexer domain
proto/                      # Protobuf definitions
  ├── trading/v1/
  ├── platform/v1/
  └── buf.yaml
migrations/                 # SQL migrations per service
  ├── trading/
  ├── platform/
  └── shared/
deploy/                     # Infrastructure configs
  ├── docker-compose.yml
  ├── prometheus.yml
  ├── grafana/
  └── nats.conf
docs/                       # Documentation
  ├── conventions.md        # AUTHORITATIVE style guide
  └── architecture.md
```

## Key Conventions

**IMPORTANT: Always read `docs/conventions.md` before writing or reviewing code.** It is the authoritative style guide.

### Idempotency
All write operations MUST be idempotent. Key source depends on operation:
- Orders: EIP-712 signature hash (cryptographic, natural key)
- Deposits: Polygon tx hash (on-chain, natural key)
- Settlements: Match ID from CLOB engine
- Withdrawals: Client-supplied key + 2FA gate + server-side duplicate check
- Affiliate claims: Server-generated from user + action

Idempotency keys checked inside the same database transaction as the write — never in a separate system.

### Error Handling
```go
// Always wrap with context
return fmt.Errorf("creating account %s: %w", id, err)

// Use errors.Is/As for inspection
if errors.Is(err, ErrNotFound) { ... }

// Domain sentinel errors map to gRPC/HTTP status codes
```

### Context
- First parameter, always: `func DoThing(ctx context.Context, ...)`
- Never store in structs
- Check `ctx.Err()` in loops
- Propagate trace context through NATS messages

### Logging
Structured fields consistently:
```go
logger.Info("order matched",
    zap.String("request_id", reqID),
    zap.String("order_id", orderID),
    zap.String("market_id", marketID),
    zap.Duration("duration", elapsed),
)
```

### Testing
- Table-driven tests with subtests and `t.Parallel()`
- Integration tests: `//go:build integration`
- TDD workflow: write tests first, use `go build` to verify compilation, run `go test` once implementation exists

### Database
```go
tx, err := pool.Begin(ctx)
if err != nil { return fmt.Errorf("beginning transaction: %w", err) }
defer tx.Rollback(ctx)
// ... do work ...
return tx.Commit(ctx)
```
- PostgreSQL with pgx driver
- Idempotency checks inside transaction
- Deterministic lock ordering to prevent deadlocks

### NATS Messaging
- Subjects: `{domain}.{action}` (e.g., `trading.match`, `indexer.deposit`)
- JetStream for durable delivery (settlements, deposits)
- Core NATS for ephemeral fan-out (book updates, price changes)
- Always propagate OpenTelemetry trace context in message headers

## Service Bootstrap Pattern

Every service follows this structure in `cmd/<service>/main.go`:
1. Create cancellable context
2. Init observability (logger + metrics + tracer)
3. Start metrics HTTP server (goroutine)
4. Connect to PostgreSQL, NATS
5. Set up signal handling (SIGINT/SIGTERM)
6. Create and start gRPC server (with reflection + health checks)
7. Block until shutdown signal or error
8. Graceful shutdown (drain NATS, close DB, stop gRPC)

## Communication Patterns

| Type | Pattern | Use Case |
|------|---------|----------|
| External → us | REST (HTTP) | Client orders, market queries, user data |
| External → us | WebSocket | Real-time book, prices, fills |
| Service → service (sync) | gRPC | Trading ↔ Platform queries |
| Service → service (async) | NATS JetStream | Matches → settlement, deposits → balance |
| Fan-out (ephemeral) | NATS Core | Book updates → WebSocket, price changes |

## Key Design Decisions

1. **Unified order book** — BUY YES @ $0.40 = SELL NO @ $0.60, doubles liquidity
2. **Off-chain matching, on-chain settlement** — instant UX, blockchain trustlessness
3. **Universal proxy wallets** — Gnosis Safe (wallet users), Poly Proxy (email users)
4. **Instant confirmation** — off-chain ledger updated on match, settlement in background
5. **NATS for all async** — JetStream (durable) + Core (ephemeral)
6. **PostgreSQL JSONB** — flexible market config, resolution parameters

## Git Conventions

- Branch format: `{issue#}-{short-description}` (e.g., `12-clob-matching-engine`)
- All work via feature branches, PRs required
- main is protected
- **Do NOT add `Co-Authored-By: Claude` or any AI attribution to commit messages**
- **NEVER commit generated code** — `proto/gen/` is in `.gitignore`

## Polymarket Compatibility

Our REST API aims for compatibility with Polymarket's CLOB client SDKs:
- TypeScript: `Polymarket/clob-client`
- Python: `Polymarket/py-clob-client`
- Order signing: `Polymarket/clob-order-utils`

## Contracts (Read-Only Reference)

ABIs consumed from the `contracts` repo via copy. No import dependency.
- CTFExchange — binary market settlement
- NegRiskCTFExchange — multi-outcome market settlement
- ConditionalTokens — token minting, splitting, merging, redemption
- Fee Module — on-chain fee collection
- Proxy Factories — user wallet deployment

## Quick Reference

| Infrastructure | Port | Purpose |
|---------------|------|---------|
| PostgreSQL | 5432 | Primary data store |
| NATS | 4222 | Messaging (client) |
| NATS | 8222 | NATS monitoring |
| Prometheus | 9090 | Metrics collection |
| Grafana | 3000 | Dashboards |
| Anvil | 8545 | Local Polygon fork |
