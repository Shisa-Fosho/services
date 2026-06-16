# Design Decisions

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
9. **No reverse proxy — direct service ports** — Services are reached directly: trading on :8080, platform on :8081. There is no longer a single unified entry point. Note: the Polymarket clob-client SDK assumed all endpoints (both session-auth and CLOB) were served from one host; with nginx removed, SDK clients must target each service port separately. Endpoint migration to consolidate this is a separate task.
10. **Naming convention** — `internal/shared/` is cross-service infrastructure. Service-specific domain code lives under the service's own path (`internal/platform/auth/`, `internal/platform/market/`, `internal/platform/affiliate/`, `internal/trading/auth/`, etc.). Top-level `internal/` entries are either **services** (`platform/`, `trading/`, `settlement/`, `indexer/`) or **cross-service infrastructure** (`shared/`) — no other category.
11. **URL-as-discriminator for type-varying admin endpoints** — when an admin operation behaves differently across market types (BINARY vs NEG_RISK), each variant gets its own URL rather than a single endpoint with an in-body `event_type` field. Concretely: `POST /admin/events/binary` vs `POST /admin/events/neg-risk` for create, `POST /admin/events/{id}/binary/markets` vs `POST /admin/events/{id}/neg-risk/markets` for one-market-at-a-time append, and `POST /admin/events/{id}/binary/resolve` vs `POST /admin/events/{id}/neg-risk/resolve` for resolve. Each handler owns one request shape (no cross-field "if BINARY then condition_id required, if NEG_RISK then absent" rules) and reads top-to-bottom without type-branching. Event create accepts one initial `market` payload (singular) because the on-chain creation flows are one condition/question per transaction; multi-market events grow through append endpoints, which also take one `market` each. There is no `markets` array anywhere in the create/append surface — the request structs carry only a singular `market`, so the obsolete `markets:[...]` shape is rejected at the JSON-decode boundary (`DisallowUnknownFields`) rather than tolerated. If on-chain batch creation is ever added, widen the shape then, not preemptively. Endpoints that don't differ between types stay singular (`PUT /admin/events/{id}`, `POST /admin/events/{id}/void`, all `/admin/markets/{id}/*`). Adding a new market type extends the URL surface, not the branching logic.
