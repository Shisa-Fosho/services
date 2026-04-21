# Shisa Services

> **Work in progress.** This repo is pre-launch and under active development.
> Architecture, APIs, service boundaries, and conventions are all subject to
> change without notice. If something here contradicts the code, trust the code.

Backend monorepo for a prediction market platform. Polymarket-style off-chain
CLOB matching with on-chain settlement on Polygon. USDC collateral.

All services are Go. Shared infrastructure (logging, metrics, tracing, Postgres,
NATS, gRPC helpers) lives in `internal/shared/` and is imported by every service.

## Services

| Service | Responsibility | HTTP | gRPC | Metrics |
|---------|----------------|------|------|---------|
| **trading** | CLOB engine, REST API, WebSocket, Polymarket-compatible API-key lifecycle | 8080 | 9001 | 9091 |
| **platform** | Session auth (SIWE / JWT / signup / login), Market API, Data API, Admin API, Affiliate | 8081 | 9002 | 9092 |
| **settlement** | Consumes matches, submits `matchOrders` on-chain, relayer | — | — | 9093 |
| **indexer** | Monitors on-chain events, publishes to NATS | — | — | 9094 |

A resolution worker is scaffolded but deferred — manual resolution is handled
by the admin wallet from the frontend.

## Architecture at a glance

```
Clients ──► Trading :8080 ──┐                  ┌──► Settlement ──► Polygon
                            ├──► NATS JetStream ┤
Clients ──► Platform :8081 ─┘                  └──► Indexer ◄────── Polygon
                   │
                   └── PostgreSQL (shared)
```

- **Trading ↔ Platform**: sync via gRPC.
- **Async fan-out**: NATS JetStream for durable work (settlements, deposits),
  core NATS for ephemeral fan-out (book updates, price ticks).
- **No reverse proxy** — clients address each service port directly.
- **Auth split**: Platform owns JWT session auth; Trading owns Polymarket-compat
  HMAC API-key auth. The two middlewares are non-overlapping by design —
  see CLAUDE.md §"Key Design Decisions".

## Tech stack

- Go 1.25, gRPC, Protocol Buffers (Buf)
- PostgreSQL 16 (pgx), NATS JetStream
- OpenTelemetry tracing (→ Tempo), zap logging (→ Loki), Prometheus metrics (→ Grafana)
- golang-migrate, Docker Compose, Foundry (local Anvil for contracts)

## Quick start

Prereqs: Docker, Go 1.25+, `make`.

```bash
make tools             # install golangci-lint, buf, protoc plugins, goimports
make up                # start Postgres, NATS, Prometheus, Grafana, Loki, Tempo + all services
make migrate-up        # run DB migrations (shared → platform → trading)
make test              # unit tests
make test-integration  # integration tests (requires `make up`)
make down              # stop + remove volumes
```

Other useful targets — `make build`, `make lint`, `make proto`, `make fmt`,
`make migrate-down`. Run `make` with no args for the full list.

## Infrastructure ports

| Service         | Port  | Purpose                             |
|-----------------|-------|-------------------------------------|
| PostgreSQL      | 5432  | Primary data store                  |
| NATS            | 4222  | Client connections                  |
| NATS monitoring | 8222  | Health + stats                      |
| Prometheus      | 9090  | Metrics collection                  |
| Grafana         | 3000  | Dashboards (anonymous admin in dev) |
| Loki            | 3100  | Log aggregation                     |
| Tempo           | 3200  | Trace storage                       |

## Repo layout

```
cmd/             # Service entry points (main.go per service)
  trading/       platform/    settlement/    indexer/    migrate/
internal/
  shared/        # Cross-service infra — observability, grpc, nats, postgres, httputil, eth
  platform/      # Platform domain: auth, market, data, admin, affiliate
  trading/       # Trading domain: order book, auth (API keys + HMAC)
  settlement/    indexer/
proto/           # Protobuf definitions (buf)
migrations/      # SQL migrations per service, run shared → platform → trading
deploy/          # docker-compose, Prometheus, Grafana, Loki, Tempo, NATS configs
docs/            # conventions.md (authoritative style guide)
```

## Conventions

Read [`docs/conventions.md`](docs/conventions.md) before writing or reviewing
code — it's the authoritative style guide for the monorepo. [`CLAUDE.md`](CLAUDE.md)
covers architectural decisions and agent guidance that complement it.

## Working on an issue

- Branch format: `{issue#}-{short-description}` (e.g. `31-admin-auth-category-crud`).
- `main` is protected; all work lands via PR.
- Generated code (e.g. `proto/gen/`) is gitignored — never commit it.
- Do **not** add AI co-author trailers to commit messages.

If you're using Claude Code with this repo's plugin set, two slash commands
automate the flow end-to-end:

- **`/begin-task`** — reads a GitHub issue, creates the correctly-named branch,
  and enters planning mode for the work.
- **`/commit-push-pr`** — stages, commits, pushes, and opens a PR in one step.
