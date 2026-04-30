# CLAUDE.md — Shisa Services

## Project Overview

Prediction market platform (Polymarket fork) — all Go backend services, shared packages, proto definitions, and infrastructure configs. Off-chain CLOB matching with on-chain Polygon settlement. USDC collateral.

## Architecture

```
┌─────────────────────────────────────────────┐
│              External Clients               │
│         (Web App, Bots, Admin App)          │
└──────────┬──────────────────┬───────────────┘
           │ :8080            │ :8081
           │ /orders, /book,  │ /auth/nonce,
           │ /ws,             │ /auth/signup,
           │ /auth/derive-    │ /auth/login,
           │ api-key,         │ /auth/refresh,
           │ /auth/api-key(s) │ /auth/logout,
           │                  │ /auth/session,
           │                  │ /admin, /markets,
           │                  │ /data
           ▼                  ▼
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
    │NATS JetStream│
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
  │   ├── ratelimit/        # Rate limiting utilities
  │   └── eth/              # Ethereum utilities (address validation, Safe address derivation)
  ├── platform/             # Platform service domain
  │   ├── auth/             #   ├─ session-auth handlers + JWT, SIWE, JWT middleware
  │   ├── market/           #   ├─ market domain
  │   ├── data/             #   ├─ data layer — users, refresh tokens, positions, SessionRepository
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
  └── architecture.md
```

## Git Conventions

- Branch format: `{issue#}-{short-description}` (e.g., `12-clob-matching-engine`)
- All work via feature branches, PRs required
- main is protected
- **Do NOT add `Co-Authored-By: Claude` or any AI attribution to commit messages**
- **NEVER commit generated code** — `proto/gen/` is in `.gitignore`

## Quick Reference

| Infrastructure | Port | Purpose |
|---------------|------|---------|
| Trading (HTTP) | 8080 | REST API, WebSocket, API-key lifecycle |
| Platform (HTTP) | 8081 | Session auth, Market API, Data API, Admin API |
| PostgreSQL | 5432 | Primary data store |
| NATS | 4222 | Messaging (client) |
| NATS | 8222 | NATS monitoring |
| Prometheus | 9090 | Metrics collection |
| Grafana | 3000 | Dashboards |
