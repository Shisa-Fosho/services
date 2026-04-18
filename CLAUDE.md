# CLAUDE.md — Shisa Services

## Project Overview

Prediction market platform (Polymarket fork) — all Go backend services, shared packages, proto definitions, and infrastructure configs. Off-chain CLOB matching with on-chain Polygon settlement. USDC collateral.

## Architecture

```
┌─────────────────────────────────────────────┐
│              External Clients               │
│         (Web App, Bots, Admin App)          │
└─────────────────┬───────────────────────────┘
                  │ All traffic
                  ▼
         ┌────────────────┐
         │  Nginx :8000   │
         │  Reverse Proxy │
         └───┬────────┬───┘
             │        │
  /orders,   │        │ /auth/nonce,
  /book,     │        │ /auth/signup,
  /ws,       │        │ /auth/login,
  /auth/     │        │ /auth/refresh,
  derive-    │        │ /auth/logout,
  api-key,   │        │ /auth/session,
  /auth/     │        │ /admin, /markets,
  api-key(s) │        │ /data
             ▼        ▼
┌─────────────────┐  ┌─────────────────┐
│ Trading Service │  │ Platform Service│
│ :8080 (HTTP)    │  │ :8081 (HTTP)    │
│ :9001 (gRPC)    │  │ :9002 (gRPC)    │
│ (metrics :9091) │  │ (metrics :9092) │
│                 │  │                 │
│ • CLOB Engine   │  │ • Session auth  │
│ • REST API      │  │   (SIWE, JWT,   │
│ • WebSocket     │  │   signup/login) │
│ • API-key       │  │ • Market API    │
│   lifecycle     │  │ • Data API      │
│   (L1 derive,   │  │ • Admin API     │
│   L2 list/      │  │ • Affiliate     │
│   revoke)       │  │                 │
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
| Nginx | Reverse proxy, route to upstream services | 8000 | — | — |
| Trading | CLOB engine, REST API, WebSocket, Polymarket-compatible API-key lifecycle (derive/list/revoke) | 8080 | 9001 | 9091 |
| Platform | Session auth (SIWE/JWT/signup/login/refresh), Market API, Data API, Admin API, Affiliate | 8081 | 9002 | 9092 |
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
  ├── indexer/
  ├── resolution/           # Resolution worker (deferred — scaffold only)
  └── migrate/              # Migration CLI tool (up/down/status)
internal/
  ├── shared/               # Cross-service infrastructure — imported by ALL services
  │   ├── observability/    # Logger, metrics, tracing, context utilities
  │   ├── grpc/             # gRPC server/client helpers, interceptors
  │   ├── nats/             # NATS client, JetStream helpers, instrumentation
  │   ├── postgres/         # Connection pooling, migration helpers
  │   ├── envutil/          # Env-var helpers (Get/MustGet) used by service main funcs
  │   ├── httputil/         # JSON helpers, HTTP middleware (RequestID, Logging, Recovery)
  │   └── eth/              # Ethereum utilities (address validation, Safe address derivation)
  ├── platform/             # Platform service domain
  │   ├── auth/             #   ├─ session-auth handlers + JWT, SIWE, JWT middleware
  │   ├── market/           #   ├─ market domain
  │   ├── data/             #   ├─ data layer — users, refresh tokens, positions, SessionRepository
  │   ├── admin/            #   ├─ admin domain
  │   └── affiliate/        #   └─ affiliate domain
  ├── trading/              # Trading service domain (Order, Trade, Book, Balance)
  │   └── auth/             #   └─ API-key lifecycle + HMAC primitives + AuthenticateAPIKey middleware
  ├── settlement/           # Settlement worker domain
  ├── indexer/              # Indexer domain
  └── resolution/           # Resolution worker domain (deferred)
proto/                      # Protobuf definitions
  ├── trading/v1/
  ├── platform/v1/
  └── buf.yaml
migrations/                 # SQL migrations per service, run in order shared → platform → trading
  ├── shared/               # Extensions + common schema (runs first)
  ├── platform/             # users, refresh_tokens, markets, positions, etc.
  └── trading/              # orders, trades, balances, api_keys (FK to users in platform)
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

### Dependencies
- **Before adding a new Go module**, always check `go.mod` and existing `internal/shared/` packages for libraries that already cover the need (including indirect dependencies that can be promoted to direct).
- Prefer using existing dependencies over adding new ones. If an existing library provides the required primitives, implement on top of it rather than pulling in a wrapper package.
- During planning, explicitly audit `go.mod` for overlap before proposing any `go get`.

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
7. **Split auth by service** — Platform service owns **session-auth endpoints** (`/auth/nonce`, `/auth/signup/*`, `/auth/login/*`, `/auth/refresh`, `/auth/logout`, `/auth/session`). Trading service owns the **Polymarket-compatible API-key lifecycle** (`/auth/derive-api-key`, `/auth/api-keys`, `/auth/api-key`). JWT verification lives in `internal/platform/auth` and is platform-only; HMAC/API-key verification lives in `internal/trading/auth` and is trading-only. This split matches Polymarket's own architectural division (gamma-api vs clob).
8. **Two non-overlapping auth middlewares** — `Authenticate` (JWT-only) for platform-owned session endpoints; `AuthenticateAPIKey` (HMAC-only, via `APIKeyReader`) for Polymarket-compat CLOB endpoints. **No endpoint accepts both.** A valid JWT on a CLOB-protected route gets 401 — enforced by a dedicated test. Rationale: keeps the auth contract unambiguous for SDK consumers and prevents the security surface from doubling on trading routes.
9. **Nginx reverse proxy** — single entry point (:8000). Exact-match `/auth/derive-api-key`, `/auth/api-keys`, `/auth/api-key` → trading; `/auth/*` prefix (everything else), `/admin`, `/markets`, `/data` → platform; `/orders`, `/book`, `/ws` → trading.
10. **Naming convention** — `internal/shared/` is cross-service infrastructure. Service-specific domain code lives under the service's own path (`internal/platform/auth/`, `internal/platform/market/`, `internal/platform/affiliate/`, `internal/trading/auth/`, etc.). Top-level `internal/` entries are either **services** (`platform/`, `trading/`, `settlement/`, `indexer/`) or **cross-service infrastructure** (`shared/`) — no other category.

## Git Conventions

- Branch format: `{issue#}-{short-description}` (e.g., `12-clob-matching-engine`)
- All work via feature branches, PRs required
- main is protected
- **Do NOT add `Co-Authored-By: Claude` or any AI attribution to commit messages**
- **NEVER commit generated code** — `proto/gen/` is in `.gitignore`

## Polymarket Compatibility

Our REST API aims for compatibility with Polymarket's CLOB client SDKs. The
upstream SDK source is the **authoritative spec** — not general knowledge,
not prior implementations, not pattern-matching from similar systems.

### Pinned reference versions

| Repo | Pinned tag | Purpose |
|------|-----------|---------|
| `Polymarket/clob-client` | `v5.8.2` | TypeScript SDK — auth headers, request signing, REST client |
| `Polymarket/py-clob-client` | `v0.34.6` | Python SDK — cross-reference for TS |
| `Polymarket/clob-order-utils` | `main` | Order signing primitives (pin when stable) |

Bump these versions intentionally when you decide to upgrade compatibility
target. Do not drift — if planning work references a newer behavior, pin
higher first.

### SDK verification rule

Before planning or implementing any Polymarket-compatible surface (auth
headers, order signing, REST endpoints, response formats):

1. **Fetch the source from the pinned tag.** Use `gh api` so the fetch is
   reproducible and tied to a specific commit:
   ```bash
   # List directory:
   gh api repos/Polymarket/clob-client/contents/src/headers?ref=v5.8.2 --jq '.[].name'

   # Read a file:
   gh api repos/Polymarket/clob-client/contents/src/headers/index.ts?ref=v5.8.2 \
     --jq '.content' | base64 -d
   ```
2. **Extract the exact contract** — header names, field names, types, ordering,
   signing payload format — and document it in the plan.
3. **Derive tests from SDK behavior, not from the implementation.** Writing
   both code and tests from your own mental model creates a closed loop
   where the tests validate the wrong assumption. Tests must assert what
   a real SDK client sends and expects, including the **absence** of fields
   the SDK doesn't use.
4. **Cross-reference both SDKs** when one is ambiguous. If `clob-client` (TS)
   and `py-clob-client` (Python) disagree, flag it to the user rather than
   picking one silently.

### Why this matters (incident log)

- **PR #48 (nonce tracker)**: first implementation added an in-memory nonce
  tracker keyed on a `POLY_NONCE` header for HMAC-signed requests. The actual
  clob-client L2 headers don't send `POLY_NONCE` — it's only used on L1
  (EIP-712) headers. Caught in review by fetching the SDK source; the entire
  nonce subsystem was removed. Rule exists to prevent recurrence.

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
| Nginx | 8000 | Reverse proxy (single entry point) |
| PostgreSQL | 5432 | Primary data store |
| NATS | 4222 | Messaging (client) |
| NATS | 8222 | NATS monitoring |
| Prometheus | 9090 | Metrics collection |
| Grafana | 3000 | Dashboards |
| Anvil | 8545 | Local Polygon fork |
