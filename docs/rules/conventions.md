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
- **Assert the failure reason, not just the status code.** When a test exists to prove a *specific* rejection (e.g. "token id mismatch → 422", "payouts not reported → 422"), asserting only `rec.Code == 422` is insufficient: several distinct checks in the same handler return the same status, so the test can pass green on the wrong one — and keep passing silently if the checks are reordered or a regression makes an earlier gate fire. Assert a substring of the error message (or the sentinel via `errors.Is`) that pins *which* check rejected:
  ```go
  if rec.Code != http.StatusUnprocessableEntity {
      t.Fatalf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
  }
  if !strings.Contains(rec.Body.String(), "does not match on-chain derivation") {
      t.Errorf("422 but not from the token-id check: %q", rec.Body.String())
  }
  ```
  Two corollaries: (1) **Setup isolation is necessary but not sufficient** — arranging state so only the target check can fire (e.g. seeding a valid slot count so the slot gate passes) is invisible to a future reader, so make the assertion carry the intent. (2) **When the response is deliberately generic** (e.g. all chain-read failures return the same 502 body), the message can't distinguish the cause; rely on setup isolation and add a comment saying so, rather than a misleading body assert. This generalises the happy-path + error-path rule above: an error-path test must assert it failed *for the reason it claims*.

#### Review Existing Tests Before Writing New Ones

When a change or refactor touches behavior, **the starting point is always the tests that already exist — never the code you just wrote.** Before adding a test, find the tests that already cover the function, handler, or resource you changed, and work forward from them.

**The required order of operations:**

1. **Find what exists first.** Search the relevant test file/section for the function or endpoint under test (e.g. grep `handler_<resource>_test.go` for the handler name, or the `// --- /admin/events POST` section markers) before writing a single new test. Tests are organised by the resource they cover, so the existing coverage is findable.
2. **Update existing tests in place when behavior changed.** If a refactor or behavior change makes a current test wrong, stale, or incomplete, **update that test** — its body, its assertions, AND its name. Do not leave the old test untouched and write a new one alongside; that produces duplicate or conflicting coverage.
3. **Only write a new test for a genuinely new outcome.** A new test is justified only when it asserts a **distinct, previously-unasserted outcome** — new functionality with no existing coverage, or a new failure/edge path that a change has opened up. "I wrote new code, so I write new tests for it" is the wrong instinct: the new code may be exercised by tests that already exist and just need updating.

**Names and intent must stay aligned.** When you update a test, re-check that its name still describes what it asserts. A test named `_Success` that has been refactored into a rejection check is worse than a duplicate — it actively lies about what's covered, and the next person trusts the name. If the assertion changed, the name changes with it. If a refactor makes an old test prove behavior that can no longer happen, **delete or repurpose it** as part of the same change — don't keep tests that assert obsolete behavior.

**Why:** writing tests scoped only to "the code I just touched" silently grows duplicate coverage, leaves misleadingly-named tests behind after refactors, and scatters one resource's coverage across files named after whatever task spawned them (see the file-naming rule below — tests belong with the resource they cover, never named after a ticket). De-duplicating against existing tests keeps the suite a faithful, navigable map of actual behavior. This is about removing redundancy, not discouraging tests: when a new outcome genuinely lacks coverage, write the test.

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

### Function Decomposition

Any function — handler, repo method, service-bootstrap routine, NATS consumer — that does multiple distinct things in sequence is a candidate for phase extraction once it grows past ~60 lines and has 3+ phases. Long top-to-bottom functions are not un-Go (the stdlib has plenty), but a function that's hard to follow because phases blur together IS a problem regardless of language.

**Where this applies in this repo:**
- HTTP handlers (parse → load → validate → mutate → publish → encode)
- Repo transactional methods (begin tx → lock → validate → write → recompute aggregates → re-read → commit)
- Service bootstrap in `cmd/*/main.go` (observability → DBs → NATS → handlers → register → block-on-signal → shutdown)
- NATS consumers (decode → validate → mutate → ack)
- Matching / settlement pipelines in the trading and settlement services

**When to extract:**
- Each helper must have a **good name** — a verb-phrase you'd want to read on the main path (`loadEventForVoid`, `verifyVoidChainState`, `lockEventForTransition`). If the best name you can come up with is `step3` or `doPart`, don't extract.
- The phase has clear input/output boundaries — typically 2-4 parameters in, 1-2 values out.
- The body is non-trivial (10+ lines, multiple early-returns, or both).

**When NOT to extract:**
- The helper would be a one-liner or near-one-liner.
- It needs 5+ parameters / 3+ return values. That's a sign the seam is wrong — find a different cut, or leave inline.
- The "phase" is just `validateRequestID()` or `writeNotFoundResponse()` — extractions that don't have meaningful names, just relocate code. That's the antipattern Go culture warns about.

**Parameter hygiene.** Helpers take what they need, not god-objects. Don't pass `*http.Request` (handlers) or `*pgxpool.Pool` (repo helpers) when a `ctx` and the specific domain values would do. Grep-friendly and limits coupling. The exception is when the helper genuinely needs to operate on the larger object (e.g. a repo helper that must use the *same* `pgx.Tx` the caller began — see below).

**Inline what stays inline.** Small error-mapping switches stay next to the call that produced the error — extracting them pushes status-code decisions away from the operation that triggered them. Same for one-line response encoding.

#### HTTP-handler addendum: the `(value, ok)` / `(ok bool)` idiom

HTTP handlers are special because `http.ResponseWriter` is a side-effect channel that doesn't propagate up the call stack like a return value does. The convention for handler helpers that may need to write an error response is:

```go
func (handler *Handler) loadEventForVoid(w http.ResponseWriter, ctx context.Context, eventID string) (*Event, bool) {
    event, err := handler.repo.GetEvent(ctx, eventID)
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
            return nil, false   // helper has already written the response
        }
        handler.internalError(w, "loading event", err)
        return nil, false
    }
    // ...
    return event, true
}

// Caller:
event, ok := handler.loadEventForVoid(w, r.Context(), eventID)
if !ok {
    return    // helper wrote the response; nothing else to do
}
```

For helpers that have only a pass/fail outcome (no value to return), use a plain `bool`. The contract is: **`ok == false` means the helper has already written the HTTP response.** The caller just returns.

**Important: do NOT use this idiom outside HTTP handlers.** In a pure Go function with no side-effect channel like `w`, returning `(value, ok)` instead of `(value, error)` loses information — the caller knows it failed but not why, can't `errors.Is` / `errors.As`, can't wrap context. The `ok bool` is a HTTP-boundary convention only; outside that boundary, errors are first-class and should be returned as errors.

`http.ResponseWriter` IS in scope for handler helpers — that's the whole point of the idiom. `*http.Request` should NOT be in scope unless the helper actually needs to read the body or URL path.

### File Organization Within a Package

Split source files to keep each one navigable, NOT to enforce visibility (Go doesn't need it) or to make types reusable (everything in the same package already is).

**When to split:**
- The file is hard to navigate because responsibilities are mixed. The trigger is "this file does N distinct things," not raw line count — a focused 800-line file is easier to read than a 200-line file mixing three resources.
- The package has multiple distinct resources, OR a single resource has multiple type-discriminated sub-actions large enough to warrant separation.

**When NOT to split:**
- A small package with one resource. Keep `handler.go` as one file.
- Don't split prophylactically. Only split once navigation actually hurts.

**Naming:** `handler_<resource>.go` per resource (e.g. `handler_categories.go`, `handler_markets.go`, `handler_events.go`). Further split as `handler_<resource>_<subaction>.go` when a resource has multiple sub-actions large enough on their own — we did this for `handler_events_create.go` and `handler_events_resolve.go` because the binary/neg-risk URL split produced two substantial endpoints per sub-action.

**The aggregator pattern.** The main `handler.go` owns:
- The `Handler` struct definition.
- `NewHandler` constructor.
- `RegisterAdminRoutes` — every `mux.Handle(...)` line stays here, even when the handler function itself is in another file. One place to read the full URL surface.
- Cross-resource error helpers (`internalError`, `chainError`, `publishFailed`).

Per-resource files own: handler functions for that resource, their request/response types, conversion functions (`toMarketResponse`), and resource-local helpers.

**Shared types live with their primary consumer.** A response type used across multiple files (e.g. `eventWithMarketsResponse` used by create, resolve, void) lives in the file whose handlers are its primary user. If there is no clear primary consumer, put the type in the aggregator file (`handler.go`) next to the cross-resource helpers.

**Generalizes beyond handlers.** The aggregator + per-resource split applies to any growing source file in the package. If `pg_repository.go` outgrows itself, the pattern is: `pg_repository.go` keeps the struct + constructor + cross-resource helpers; `pg_repository_events.go`, `pg_repository_markets.go`, etc. own methods for those resources. Same logic for NATS consumers, settlement/matching pipelines, service bootstrap helpers.

**Test file naming.** Two valid options: a single `<package>_test.go` for the whole package, OR mirroring the source split (`handler_markets_test.go`, `handler_events_test.go`, etc.). Either is idiomatic Go. Mirror the split when a single test file would exceed ~1500 lines or when test helpers cluster by resource. Otherwise one file is fine — pick whichever serves readability.

### Dependencies

- **Before adding a new Go module**, always check `go.mod` and existing `internal/shared/` packages for libraries that already cover the need (including indirect dependencies that can be promoted to direct).
- Prefer using existing dependencies over adding new ones. If an existing library provides the required primitives, implement on top of it rather than pulling in a wrapper package.
- During planning, explicitly audit `go.mod` for overlap before proposing any `go get`.

### Dependency Pinning

**Every dependency — library or tool, current or future — MUST be pinned to an explicit version. Never `@latest`, never an unpinned floating ref.** This applies to:

- **Go module dependencies** — managed by `go.mod`/`go.sum`, which pin by construction. Don't defeat this with `go get module@latest` in scripts; if you bump a dep, commit the resulting `go.mod`/`go.sum` change.
- **Developer tools installed via `go install`** (in the `Makefile` `tools` target or anywhere else) — every `go install` MUST carry an explicit `@vX.Y.Z`. The versions live in named Makefile variables (`GOLANGCI_LINT_VERSION`, `BUF_VERSION`, etc.) so they're visible and auditable in one place.
- **CI actions, Docker base images, and any other external artifact** — pin to a tag or digest, never `latest`.

**Why:** `@latest` makes builds non-reproducible and silently imports breaking changes — a green build today can break tomorrow with no code change. A concrete failure this rule exists to prevent: `make tools` installed `golangci-lint@latest`, which resolved to a v1 release built against Go 1.24 while the repo targets Go 1.25.5; the older-Go-built linter could not typecheck 1.25 language constructs, so `make lint` broke through no fault of our code.

**Bumping a pinned version** is a deliberate act: do it in its own commit, with the version change visible in the diff, after verifying the new version builds, tests, and lints clean. Never bump as a drive-by inside an unrelated change.

**Tool toolchain floor.** Tools that embed the Go typechecker (golangci-lint especially) must be **built with a Go toolchain no older than the repo's `go` directive**, or they can't analyze the repo's language version. The `Makefile` enforces this by setting `GOTOOLCHAIN=$(go env GOVERSION)+auto` for every tool install — a floor, not a hard pin, so tools whose own `go.mod` requires a newer patch (e.g. buf) can still upgrade, while nothing is ever built with an older toolchain than the repo targets.

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
5. Service handlers depend on the shared reader interface directly (`eth.CTReader`, `eth.NegRiskReader`). Don't redeclare a local-package mirror unless the consumer genuinely uses a strict subset of the shared interface's methods — at that point a local interface earns its keep by shrinking the test fake. Mirroring a shared interface verbatim is the parallel-API-symmetry antipattern called out in "No Speculative Code" above.

**Rules of thumb:**

- One file per contract under `internal/shared/eth/` for the wrapper. Don't combine multiple contracts into one file — each is independently versioned.
- Keep shared reader interfaces narrow at the source. If a shared reader is growing past what most consumers need, split it (e.g. `CTReader` vs `CTWriter`) rather than asking every consumer to declare a local subset.
- Handler tests fake the shared interface. They never import the `gen/` packages.
- Adding a new contract: drop the ABI JSON in `abi/`, add a `//go:generate` line in `generate.go`, write the wrapper, write the narrow shared interface. Run `make gen-contracts`.
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
