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
