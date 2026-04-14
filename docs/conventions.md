# Development Conventions

> This is the authoritative style guide for the Shisa services monorepo. When in doubt, defer to this document.

## General Principles

1. **Clarity over cleverness** — readable code wins
2. **Fail fast, validate early** — reject bad input at the boundary
3. **Idempotency for all writes** — key source varies by operation (see CLAUDE.md)
4. **Observable from day one** — structured logs + metrics + traces on every service
5. **Test what matters** — domain logic thoroughly, integration paths with real dependencies

## Go Style

### Code Organization
- Standard Go project layout
- Internal packages for non-exported code
- Domain logic in `internal/<service>/`
- Shared infrastructure in `internal/platform/`

### Naming
- MixedCaps/camelCase (never underscores in Go identifiers)
- Interface names: method name + "er" suffix for single-method interfaces (e.g., `Reader`, `Matcher`)
- Receiver names: short, 1-2 chars, consistent within type (e.g., `s` for Server, `r` for Repository, `e` for Engine)
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
- Shared test helper: `postgres.TestPool(t)` from `internal/platform/postgres/testutil.go`
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
- Profile before optimizing
- Readability over micro-optimizations
- Only trade readability for performance on proven hot paths (matching engine inner loop, WebSocket fan-out)
- For low-volume operations (market creation, user signup), prefer clarity

### Security
- Never log sensitive data
- Validate all inputs at service boundary
- Parameterized queries only
- TLS for all network communication
- Set timeouts on all external calls (RPC, DB, blockchain)
- EIP-712 signature verification for all order operations
