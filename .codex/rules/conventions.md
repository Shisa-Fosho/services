# Development Conventions

> This is the authoritative style guide for the Shisa services monorepo. When in doubt, defer to this document.

## General Principles

1. **Clarity over cleverness** — readable code wins
2. **Fail fast, validate early** — reject bad input at the boundary
3. **Idempotency for all writes** — key source varies by operation; checked inside the same DB transaction as the write, never separately
4. **Observable from day one** — structured logs + metrics + traces on every service
5. **Test what matters** — domain logic thoroughly, integration paths with real dependencies

## Go Style

### Code Organization
- Standard Go project layout
- Internal packages for non-exported code
- Domain logic in `internal/<service>/`
- Shared infrastructure in `internal/shared/`

### Naming
- MixedCaps/camelCase (never underscores in Go identifiers)
- Interface names: method name + "er" suffix for single-method interfaces (e.g., `Reader`, `Matcher`)
- **All names (variables, parameters, receivers, struct fields, locals — anything that needs a name) must use full or, at worst, partial words that convey meaning.** Single-letter and 1-2 char abbreviations are prohibited. Examples:
  - Receivers: `server` (not `s`), `repo` (not `r`), `engine` (not `e`), `handler` (not `h`), `book` (not `b`)
  - Locals: `user` (not `u`), `order` (not `o`), `err` is allowed (idiomatic Go), `ctx` is allowed (idiomatic Go)
  - Loop variables: `idx`/`index` (not `i`), `item` (not `v`) — even short loops should be readable
  - The only exceptions are the universally idiomatic Go names: `ctx` (context.Context), `err` (error), `tx` (transaction), `ok` (bool from comma-ok idiom), `w`/`r` ONLY when they are `http.ResponseWriter` / `*http.Request` in a handler signature (standard library idiom)
- Package names: short, lowercase, no underscores, no plural (e.g., `market`, `trading`, `settlement`)

### Error Handling
```go
// Wrap with context using %w for unwrapping
return fmt.Errorf("posting transaction %s: %w", txID, err)

// Domain sentinel errors
var (
    ErrNotFound          = errors.New("not found")
    ErrInsufficientFunds = errors.New("insufficient funds")
    ErrMarketPaused      = errors.New("market is paused")
    ErrOrderExpired      = errors.New("order expired")
)

// Rich error types when context matters
type InsufficientFundsError struct {
    UserAddress string
    Required    int64
    Available   int64
}

// Check with errors.Is/As — never string comparison
if errors.Is(err, ErrNotFound) { ... }

// Map domain errors to HTTP/gRPC status codes at the handler boundary
```

### Context
- Always first parameter: `func DoThing(ctx context.Context, ...)`
- Never store in structs
- Use typed keys for context values (not strings)
- Check `ctx.Err()` in loops and long operations
- Propagate trace context through NATS messages

```go
type contextKey string
const requestIDKey contextKey = "request_id"

func WithRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, requestIDKey, id)
}
```

### Logging (Structured)
```go
logger.Info("order matched",
    zap.String("request_id", reqID),
    zap.String("order_id", orderID),
    zap.String("market_id", marketID),
    zap.String("user_address", addr),
    zap.Int64("price", price),
    zap.Int64("size", size),
    zap.Duration("duration", elapsed),
)
```
Standard fields: `request_id`, `user_address`, `order_id`, `market_id`, `tx_id`, `idempotency_key`, `duration`, `error`

**Never log:** private keys, HMAC secrets, 2FA codes, full signatures, passwords, session tokens.

### Testing
- Table-driven with subtests: `t.Run(name, func(t *testing.T) { ... })`
- `t.Parallel()` for independent unit tests; **avoid `t.Parallel()` in integration tests** that share a database — parallel truncation causes test pollution
- Integration tests: `//go:build integration` (requires running stack)
- Integration test runs across packages must use `-p 1` because packages share one physical database
- Shared test helper: `postgres.TestPool(t)` from `internal/shared/postgres/testutil.go`
- TDD: write tests first, `go build` to verify compilation, `go test` once implementation exists
- Test naming: `TestFunctionName_Scenario` (e.g., `TestMatchOrders_InsufficientBalance`)
- **Handler tests required** — every HTTP handler must have handler-level tests using `httptest.NewRecorder` + real mux routing. At minimum: one happy-path and one error-path test per endpoint. Auth-required endpoints must also test missing/invalid JWT.

### Validation
Domain validators (`ValidateUser`, `ValidateMarket`, `ValidatePosition`, etc.) are package-level functions in `internal/<domain>/validate.go` that return an error wrapping a domain sentinel (e.g., `ErrInvalidPosition`).

**Where to call them:**
- **Shape validators** — those that only inspect struct fields (`ValidateUser`, `ValidateMarket`, `ValidateEvent`, `ValidateReferral`, `ValidatePosition`, `ValidateEarning`) are called **inside the repository write method** (`CreateX`, `UpsertX`, `RecordX`). This gives "data reaching the DB is valid" as a property of the repository, so no caller can forget to validate.
- **Business-rule validators** — those that need external context the repository doesn't have (`ValidateOrder` takes `MarketConfig` and `time.Time`) are called **by the caller** before invoking the repository. Repository doc comments state the contract.

Validators remain exported so that future boundary layers (REST handlers, gRPC services, NATS consumers) can also invoke them for early input rejection and clean API error messages. Calling the same validator at both the boundary and inside the repository is fine — validation is cheap and double-checking is never wrong.

**Rule of thumb**: if a validator only looks at the struct fields, the repo calls it. If it needs external context, the caller calls it.

### Database (PostgreSQL)
```go
// Deferred rollback pattern (pgx)
tx, err := pool.Begin(ctx)
if err != nil { return fmt.Errorf("beginning transaction: %w", err) }
defer tx.Rollback(ctx)

// ... do work ...

return tx.Commit(ctx)
```
- Use pgx driver (not database/sql)
- Parameterized queries only (SQL injection prevention)
- Idempotency checks inside the same transaction as writes
- Deterministic lock ordering for concurrent access
- JSONB for flexible config (market resolution params, etc.)
- Migrations via golang-migrate

### Migrations Are Append-Only After Merge

**NEVER edit a migration that has been merged to `main`.** Once merged, a migration has been applied to every developer's dev DB and (eventually) every environment up to production. The migration tool tracks applied migrations by version number — it does NOT re-run a migration whose source has changed. So editing a merged migration creates **silent schema drift**: fresh DBs get the new shape, existing DBs stay on the old shape, and there is no migration that can bridge them.

To change the schema after a migration has merged, **add a new migration** with the next version number that ALTERs forward from the current state. This is true even if the change feels "small" or "fixing a typo" — every migration is frozen the moment it lands on `main`.

In production this drift is a disaster: you cannot drop and recreate the DB. The only recoveries are an emergency forward migration written under pressure, or a hand-written backfill script — both riskier and slower than doing it right the first time.

#### Editing a Migration Before It Has Merged

While a migration is on your branch and unmerged, you may iterate on it — but you MUST keep your local DB synced with the migration source. The required workflow:

```bash
make migrate-down   # roll back the migration you're about to edit
# edit the .up.sql / .down.sql files
make migrate-up     # re-apply the new version
```

Without the `migrate-down` step, the migration tool sees the old version as already applied and skips it on `migrate-up`, leaving your dev DB on the previous shape while the source says otherwise. This is the same drift pattern as editing-after-merge — it just happens locally first.

If a `down` migration would drop test data you care about, copy it out before running down, then restore after up. Most "test data" is regeneratable from seed scripts; if it isn't, the seed scripts are the bug.

#### Why This Matters For `docs/schema.sql`

`docs/schema.sql` is generated by `pg_dump` against the local dev DB. The dump is canonical only if the dev DB matches the migration source. Down-before-edit is what keeps that invariant true. If anyone violates it, their dev DB drifts and any schema dump from that machine corrupts main's schema.sql.

### NATS Messaging
- Subject naming: `{domain}.{action}` (e.g., `trading.match`, `indexer.deposit.confirmed`)
- JetStream streams for durable delivery (settlements, deposits, resolutions)
- Core NATS for ephemeral fan-out (book updates, price ticks)
- Always include trace context in message headers
- Consumer names: `{service}-{action}` (e.g., `settlement-match-consumer`)

### API Design
- REST: standard HTTP status codes, JSON, idempotency keys for writes
- gRPC: protobuf, appropriate status codes, server reflection enabled
- All write endpoints require authentication
- Public read endpoints (market data) are unauthenticated
- Pagination: cursor-based for lists, not offset-based

### Dependencies

- **Before adding a new Go module**, always check `go.mod` and existing `internal/shared/` packages for libraries that already cover the need (including indirect dependencies that can be promoted to direct).
- Prefer using existing dependencies over adding new ones. If an existing library provides the required primitives, implement on top of it rather than pulling in a wrapper package.
- During planning, explicitly audit `go.mod` for overlap before proposing any `go get`.

### Imports
```go
import (
    // Standard library
    "context"
    "fmt"

    // Third-party
    "github.com/jackc/pgx/v5"
    "go.uber.org/zap"

    // Internal
    "github.com/Shisa-Fosho/services/internal/trading"
)
```

### Performance

Default to the simplest correct implementation. Add complexity only on proven hot paths.

| Call frequency | Examples | Rule |
|----------------|----------|------|
| Hot (high req/s) | Order matching, book fan-out | Optimize deliberately |
| Warm | Trade history queries | Index + simple query |
| Cold (rarely called) | Admin metadata updates, market creation | Simplest possible |

**Before reaching for a complex implementation, ask:**
- How often is this called? (Per-second vs. once a week?)
- What does the naive approach actually cost at our scale?
- Is this optimizing a real measured problem, or an imagined future one?

**Antipattern:** A 50-line dynamic SQL builder that skips updating unchanged columns on a table that sees one write per month. A short, readable query that covers all cases is correct and fast enough permanently.

### Idempotency

All write operations MUST be idempotent. Key source depends on operation:
- Orders: EIP-712 signature hash (cryptographic, natural key)
- Deposits: Polygon tx hash (on-chain, natural key)
- Settlements: Match ID from CLOB engine
- Withdrawals: Client-supplied key + 2FA gate + server-side duplicate check
- Affiliate claims: Server-generated from user + action

Idempotency keys checked inside the same database transaction as the write — never in a separate system.

### No Speculative Code

Ship only code that is reached from real call sites within the current issue's scope. "Speculative" isn't limited to whole functions — the rule covers:

- **Exported functions, methods, and option constructors** (`WithFoo`, `NewBar`). Go's unused-code detection only fires on *internal* identifiers — exported ones accumulate silently. You must check with grep, not trust the compiler.
- **Parameters, struct fields, and interface methods** that no caller populates or reads. An unused `MiddlewareOption`, config field, or context key counts.
- **Parallel-API symmetry** across packages. If package A has a `WithAuthFailureHook` and package B's version has no caller, don't mirror it into B "for consistency" — that's dead code dressed up as tidiness.
- **Placeholder hooks, feature flags, and config fields** wired up "in case" a future issue needs them.

If a future issue genuinely needs the missing piece, the build error (internal) or grep (exported) will surface it in seconds. Document expected prerequisites in the issue description — not in unreachable code.

### On-chain Contract Bindings

**Never hand-roll Solidity ABIs as Go string constants.** All contract interactions go through `abigen`-generated bindings produced from the canonical ABI JSON. The full pipeline:

1. The ABI JSON lives under `internal/shared/eth/abi/<Contract>.json`, vendored from `shisa-contracts/abi/` (which exports them via `forge build`'s `extra_output_files`).
2. `abigen` produces Go bindings under `internal/shared/eth/gen/<contract>/` from a `//go:generate` directive in `internal/shared/eth/generate.go`.
3. The `gen/` directory is `.gitignore`d — generated code is regenerated on every fresh clone via `make build` (which depends on `make gen-contracts`).
4. A thin reader wrapper in `internal/shared/eth/<contract>.go` exposes a narrow `*Reader` interface (e.g. `CTReader`, `NegRiskReader`) that hides the abigen verbosity and provides the surface handlers actually need.
5. Service handlers declare *their own* local interface that is a subset of the shared reader, so the test fake stays minimal and the dependency surface is auditable per service.

**Rules of thumb:**

- One file per contract under `internal/shared/eth/` for the wrapper. Don't combine multiple contracts into one file — each is independently versioned.
- The narrow reader interface ships in `internal/shared/eth/`. Per-service handler interfaces are local to that handler's package (not exported).
- Handler tests fake the local interface, not the shared one. They never import the `gen/` packages.
- Adding a new contract: drop the ABI JSON in `abi/`, add a `//go:generate` line in `generate.go`, write the wrapper, write the narrow interface. Run `make gen-contracts`.
- Refreshing an ABI when upstream contracts change: rebuild `shisa-contracts` (`forge build`), copy the ABI JSON, run `make gen-contracts`. Never edit ABI JSON by hand.
- `gen/` is regenerated by every developer's `make build`. Don't `git add` files under it.

This pattern exists because hand-rolled ABI JSON literals and bind.Call dispatch invite typos that the compiler can't catch — wrong parameter names, wrong return types, mis-decoded `[32]byte` vs `common.Hash`. Catching those at codegen time instead of at runtime is a strict win.

### Keeping CLAUDE.md In Sync

Whenever you add, remove, or rename a directory as part of a task, ask the user
whether they'd like to update the Code Organization section in `CLAUDE.md` to
reflect the change. Don't update it silently — the user may have intentionally
omitted a directory, or the change may be temporary.

### Security
- Never log sensitive data
- Validate all inputs at service boundary
- Parameterized queries only
- TLS for all network communication
- Set timeouts on all external calls (RPC, DB, blockchain)
- EIP-712 signature verification for all order operations
