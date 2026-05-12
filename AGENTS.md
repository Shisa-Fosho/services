# AGENTS.md - Shisa Services Codex Setup

This is the Codex entry point for the Shisa-Fosho services repository. The
Claude setup is preserved in `.claude/`; do not replace, remove, or rewrite it
when updating Codex configuration. Codex-specific mirrors live under `.codex/`.

## Project Overview

Prediction market platform backend services, shared packages, protobuf
definitions, migrations, and infrastructure configs. The system is a Polymarket
style fork: off-chain CLOB matching, on-chain Polygon settlement, and USDC
collateral.

Primary services:

| Service | Responsibility | HTTP | gRPC | Metrics |
| --- | --- | --- | --- | --- |
| Trading | CLOB engine, REST API, WebSocket, Polymarket-compatible API-key lifecycle | 8080 | 9001 | 9091 |
| Platform | Session auth, market API, data API, admin API, affiliate | 8081 | 9002 | 9092 |
| Settlement Worker | On-chain trade settlement, relayer | - | 9003 | 9093 |
| Indexer | On-chain event monitoring, deposits | - | 9004 | 9094 |

Resolution worker automation is deferred; manual resolution is handled through
admin wallet flows plus backend verification.

## Essential Commands

```bash
make up               # Start local stack
make down             # Stop and clean up
make build            # Compile all services
make test             # Run unit tests
make test-integration # Integration tests; requires stack
make lint             # golangci-lint + go vet
make proto            # Generate protobuf code
make fmt              # gofmt + goimports
make migrate-up       # Run migrations
make migrate-down     # Roll back last migration
make tools            # Install dev tools
```

## Code Organization

- `cmd/`: service entry points.
- `internal/shared/`: cross-service infrastructure used by all services.
- `internal/platform/`: platform service domains such as auth, market, data,
  admin, and affiliate.
- `internal/trading/`: trading service domain, CLOB data, and API-key auth.
- `internal/settlement/`, `internal/indexer/`, `internal/resolution/`: worker
  domains.
- `proto/`: protobuf definitions and Buf config.
- `migrations/`: SQL migrations, ordered `shared` then `platform` then
  `trading`.
- `deploy/`: Docker Compose and observability infrastructure.
- `docs/`: architecture, conventions, schema, and planning docs.

When adding, removing, or renaming directories, ask the user whether to update
the Code Organization section in `CLAUDE.md` and this file. Do not silently
rewrite those sections.

## Codex Mirrors

Full project rules ported from Claude live here:

- `.codex/rules/conventions.md`: authoritative development conventions.
- `.codex/rules/design-decisions.md`: service bootstrap, communication
  patterns, and architectural decisions.
- `.codex/rules/polymarket.md`: pinned Polymarket compatibility references and
  verification rules.
- `.codex/skills/`: Codex skill mirrors of the Claude workflows.
- `.codex/agents/quick-task.md`: Codex-adapted quick-task worker instructions.

Read the relevant `.codex/rules/*.md` before making substantive changes. The
root `CLAUDE.md` remains a source document for project context and should be
kept in sync when the user explicitly approves.

## Development Rules

- Favor clarity over cleverness and simple correct implementations over
  speculative abstractions.
- Validate inputs at service boundaries and inside repository write methods
  where validators only inspect struct shape.
- All writes must be idempotent, with idempotency checks inside the same
  database transaction as the write.
- Use structured logs, metrics, and tracing. Never log private keys, HMAC
  secrets, passwords, session tokens, full signatures, or similar sensitive
  values.
- Use pgx and parameterized SQL only.
- Migrations are append-only after merge. If a migration is already merged to
  `main`, add a new migration instead of editing it.
- If editing an unmerged migration, run `make migrate-down`, edit, then
  `make migrate-up` so the local DB matches the source.
- Never hand-roll Solidity ABIs as Go string constants. Use vendored ABI JSON
  plus abigen-generated bindings and narrow wrapper interfaces.
- Do not add speculative exported functions, options, config fields, hooks, or
  mirrored APIs with no real caller. Check exported usage with search.
- Before adding a Go dependency, inspect `go.mod` and `internal/shared/` for an
  existing suitable library.

## Go Style

- Use meaningful names. Avoid single-letter variables except idiomatic `ctx`,
  `err`, `tx`, `ok`, and `w`/`r` for HTTP handler signatures.
- Keep context as the first parameter, never stored in structs.
- Wrap errors with `%w`; use `errors.Is` and `errors.As`, never string
  comparisons.
- Domain errors should be mapped to HTTP or gRPC statuses at the handler
  boundary.
- Handler tests are required for HTTP handlers: at least happy path and error
  path, plus missing/invalid JWT for auth-required endpoints.
- Integration tests use `//go:build integration`; avoid `t.Parallel()` when a
  shared DB is involved. Cross-package integration runs need `-p 1`.

## Git And PR Rules

- Branch format: `{issue#}-{short-description}`.
- Create new branches from freshly fetched `origin/main`, not the current HEAD.
- Do not add AI attribution to commits or PRs.
- Never commit generated code under `proto/gen/`, secrets, `.env` files,
  binaries, `node_modules`, or vendor directories.
- If `migrations/**/*.sql` changes, regenerate and stage `docs/schema.sql`
  before committing.
- Before pushing a task branch, compare `git diff origin/main...HEAD --stat`
  against the promised plan and report any mismatch.

## Polymarket Compatibility

For Polymarket-compatible behavior, upstream SDK and contract sources at pinned
versions are the spec. Fetch and inspect the pinned source before implementing
auth headers, order signing, endpoint shapes, response formats, fee behavior, or
contract interactions. Derive tests from SDK behavior, not from the local
implementation.

Pinned details and incident notes are in `.codex/rules/polymarket.md`.
