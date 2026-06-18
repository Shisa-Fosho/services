# Admin Panel Integration Guide

> **Audience:** frontend / admin-panel implementers and agent operators who need
> to drive the market lifecycle from a UI. This document is the API contract —
> you should not need to read the Go backend to build admin flows.
>
> **Scope:** the `/admin/*` surface on the **platform service**. It covers auth,
> base URLs, every admin endpoint, on-chain prerequisites, retry semantics, and
> end-to-end lifecycle examples for binary and NegRisk markets.

## TL;DR for the implementer

1. **Which service?** All `/admin/*` routes live on the **platform service**, not
   the trading/CLOB service. Use the platform base URL.
2. **How do I authenticate?** `Authorization: Bearer <platform JWT>`. The JWT
   subject (wallet address) must be present in the `admin_wallets` table.
3. **What changed from the old API?** Event creation is one market at a time.
   There is no `markets: [...]` array anywhere — create and append both take a
   singular `market` object. The URL discriminates binary vs NegRisk, not an
   in-body field.
4. **What must happen on-chain first?** The admin wallet must submit the
   relevant Polygon transaction (prepare condition, report payouts) *before*
   calling the backend. The backend verifies on-chain state read-only; it never
   writes to chain.
5. **Got a 502?** If the message ends in `please retry`, the DB write succeeded
   but a downstream publish failed (or a chain read failed). Retry the exact
   same call — writes are idempotent.

---

## 1. Base URL discovery

### Staging (initial integration target)

The primary integration target is the staging deployment on the Hermes VPS. It
is **private behind Tailscale** — you must be on the relevant Tailnet before any
request will connect.

| Surface | Base URL |
|---------|----------|
| Platform (auth, admin, market data) | `https://hermes-clam-server.taileedc49.ts.net:8444` |
| Trading / CLOB | `https://hermes-clam-server.taileedc49.ts.net:8443` |

All `/admin/*` endpoints use the **platform** URL (`:8444`).

**Smoke check** — hit healthz first:

```bash
curl -s https://hermes-clam-server.taileedc49.ts.net:8444/healthz
# -> ok
```

If you get a DNS or connection error (not an HTTP error), you are not on the
Tailnet. Off-tailnet users should expect connectivity failure, not an
application error.

**Access model:**
- VPS Tailscale identity: `hermes-clam-server.taileedc49.ts.net`, IP `100.123.227.5`.
- Docker publishes only Caddy HTTPS ports bound to the Tailscale IP:
  `100.123.227.5:8443` and `100.123.227.5:8444`.
- No app, database, NATS, gRPC, or metrics ports are exposed publicly.

**Source-of-truth files for staging config** (in this repo):
- `deploy/staging/README.md` — endpoint shape, deploy model, required secrets.
- `deploy/staging/docker-compose.yml` — service composition and port bindings.
- `deploy/staging/Caddyfile` — HTTPS reverse-proxy config.
- Runtime `.env` on the VPS at `/opt/shisa/staging/.env` contains secrets —
  **never copy secret values into docs, issues, or frontend code.**

### Local development

For local development, run the full stack with `make up`. The platform service
listens on plain HTTP:

| Surface | Base URL |
|---------|----------|
| Platform | `http://localhost:8081` |
| Trading / CLOB | `http://localhost:8080` |

Smoke check: `curl -s http://localhost:8081/healthz` → `ok`.

Local admin auth requires the same JWT + `admin_wallets` setup. See the
migration files under `migrations/platform/` for the `admin_wallets` table
schema and seed your dev wallet there.

---

## 2. Authentication

All `/admin/*` routes require:

1. **A platform JWT** in the `Authorization: Bearer <token>` header.
2. The JWT subject (the authenticated wallet address) must exist in the
   `admin_wallets` table.

**Middleware chain** (outermost first):

```
Authenticate (JWT) → RequireAdmin (admin_wallets lookup) → rate limit → AuditAdminAction → handler
```

- `Authenticate` validates the Bearer JWT and extracts the wallet address into
  the request context. JWT-only — it does **not** accept trading-service
  `POLY_*` HMAC/API-key headers.
- `RequireAdmin` checks the wallet address against `admin_wallets`. If the
  address is missing or not an admin, the request is rejected before it reaches
  the handler.

**These admin routes do not accept trading-service `POLY_*` headers.** A valid
JWT on a CLOB-protected route gets 401 and vice versa — the two auth systems are
non-overlapping by design.

### Auth error responses

| Status | Meaning |
|--------|---------|
| 401 | Missing or invalid `Authorization: Bearer` token. |
| 403 | Authenticated wallet is not in `admin_wallets`. |
| 429 | Admin rate limit exceeded (per-user bucket). |

---

## 3. JSON and error conventions

### Request decoding

- Request bodies are decoded with `DisallowUnknownFields()` — unknown JSON keys
  are rejected with 400. This means the obsolete `markets: [...]` array shape is
  rejected at the decode boundary, not silently ignored.
- Maximum request body size: **1 MB**.
- Empty body where a body is required → 400 `request body is empty`.

### Error envelope

All errors return:

```json
{ "error": "<message>" }
```

### Status code reference

| Status | When |
|--------|------|
| 400 | Malformed JSON, empty body, unknown fields, or field-level validation failure (missing required field, bad format). |
| 401 | Missing or invalid Bearer token. |
| 403 | Authenticated wallet is not an admin. |
| 404 | Target record (event, market, category) not found. |
| 409 | State transition conflict or slug/condition_id/question_id already in use. Event is terminal. |
| 422 | Request is syntactically valid but conflicts with on-chain or domain state (condition not prepared, token ids don't match on-chain derivation, payouts not reported, payout numerators don't match declared outcome, market doesn't belong to event, event type mismatch). |
| 502 | On-chain read failed, or DB write succeeded but downstream publish (NATS KV / Core NATS) failed. Message ends in `please retry`. See [§7 Retry semantics](#7-retry-semantics). |
| 500 | Unexpected internal error (rare; check server logs). |

---

## 4. Event and market model

### One market at a time

Event creation accepts **exactly one initial market**. Multi-market events grow
through append endpoints, which also take one market each. This matches the
on-chain reality: each condition/question is created in its own transaction.

There is no `markets: [...]` array anywhere in the create or append surface.
Both create and append use a singular `market` object:

```json
{ "market": { ... } }
```

### URL as type discriminator

When an admin operation behaves differently across market types (binary vs
NegRisk), each variant gets its own URL — not a single endpoint with an in-body
`event_type` field:

| Operation | Binary URL | NegRisk URL |
|-----------|-----------|-------------|
| Create event | `POST /admin/events/binary` | `POST /admin/events/neg-risk` |
| Append market | `POST /admin/events/{id}/binary/markets` | `POST /admin/events/{id}/neg-risk/markets` |
| Resolve | `POST /admin/events/{id}/binary/resolve` | `POST /admin/events/{id}/neg-risk/resolve` |

Operations that don't vary by type stay singular:
- `PUT /admin/events/{id}` — update event metadata
- `POST /admin/events/{id}/void` — void (binary only; NegRisk void is unsupported)
- All `/admin/markets/*` endpoints

### Market types

- **Binary** — a two-outcome (YES/NO) market backed by a single
  ConditionalTokens condition. The admin prepares the condition on-chain and
  supplies `condition_id` + `question_id` + token ids.
- **NegRisk** — a multi-outcome market backed by the NegRisk adapter. The
  event carries a `neg_risk_market_id`; each question/market carries a
  `question_id` whose first 31 bytes match the event's market id. The backend
  derives `condition_id` server-side — the admin never supplies it.

### ID formats

| ID type | Format | Example |
|---------|--------|---------|
| `condition_id` | `0x`-prefixed 32-byte hex (66 chars) | `0xabc...def` |
| `question_id` | `0x`-prefixed 32-byte hex (66 chars) | `0x123...456` |
| `neg_risk_market_id` | `0x`-prefixed 32-byte hex, final byte `00` | `0x789...000` |
| `token_id_yes` / `token_id_no` | Canonical decimal uint256 string | `7384738291...` |
| `event_id` / `market_id` / `category_id` | Backend-generated record ID (string) | Use the value returned by the backend |

**Token IDs must be the canonical decimal rendering** — no leading zeros, no
`+` sign, no `0x` prefix. The backend matches these strings against on-chain
derivations, so only one rendering per value is accepted.

---

## 5. Response field reference

### Event

```json
{
  "id": "evt_abc123",
  "slug": "us-election-2026",
  "title": "US Election 2026",
  "description": "Who will win the 2026 US presidential election?",
  "category_id": "cat_politics",
  "event_type": "BINARY",
  "status": "ACTIVE",
  "end_date": "2026-11-04T00:00:00Z",
  "featured": false,
  "featured_sort_order": 0,
  "neg_risk_market_id": null
}
```

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Backend-generated. Use in subsequent calls. |
| `slug` | string | Unique. |
| `title` | string | |
| `description` | string | |
| `category_id` | string | FK to category. |
| `event_type` | string | `"BINARY"` or `"NEG_RISK"`. |
| `status` | string | Event lifecycle status. |
| `end_date` | string | RFC3339, UTC. |
| `featured` | bool | |
| `featured_sort_order` | int16 | |
| `neg_risk_market_id` | string? | Present only on NegRisk events. |

### Market

```json
{
  "id": "mkt_xyz789",
  "slug": "will-incumbent-win",
  "event_id": "evt_abc123",
  "question": "Will the incumbent win?",
  "outcome_yes_label": "Yes",
  "outcome_no_label": "No",
  "token_id_yes": "7384738291...",
  "token_id_no": "4829103746...",
  "condition_id": "0xabc...def",
  "question_id": "0x123...456",
  "status": "ACTIVE",
  "outcome": null,
  "price_yes": 50,
  "price_no": 50,
  "volume": 0,
  "open_interest": 0,
  "fee_rate_bps": null,
  "tick_size": "0.01",
  "min_size": 5,
  "max_size": null
}
```

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Backend-generated. |
| `slug` | string | Unique. |
| `event_id` | string | FK to event. |
| `question` | string | |
| `outcome_yes_label` / `outcome_no_label` | string | |
| `token_id_yes` / `token_id_no` | string | Decimal uint256. |
| `condition_id` | string | Hex. Derived server-side for NegRisk. |
| `question_id` | string | Hex. |
| `status` | string | See [§7 Live status overlay](#7-retry-semantics) for the KV overlay on GET. |
| `outcome` | string? | `"YES"` or `"NO"` after resolution; `null` otherwise. |
| `price_yes` / `price_no` | int64 | |
| `volume` / `open_interest` | int64 | |
| `fee_rate_bps` | int64? | Per-market fee override. |
| `tick_size` | string | |
| `min_size` | int64 | |
| `max_size` | int64? | |

### Category

```json
{
  "id": "cat_politics",
  "name": "Politics",
  "slug": "politics"
}
```

---

## 6. Endpoint reference

Every route below is registered in `RegisterAdminRoutes` (`internal/platform/market/handler.go`)
and requires the admin auth middleware (see [§2](#2-authentication)).

### Categories

#### `POST /admin/categories`

Create a category.

**Request body:**
```json
{
  "name": "Politics",
  "slug": "politics"
}
```

**Success:** `201 Created`
```json
{ "id": "cat_politics", "name": "Politics", "slug": "politics" }
```

**Errors:** 400 (missing name/slug), 409 (slug in use).

---

#### `PUT /admin/categories/{id}`

Update a category's name and slug.

**Request body:**
```json
{
  "name": "Politics & Elections",
  "slug": "politics-elections"
}
```

**Success:** `200 OK`
```json
{ "id": "cat_politics", "name": "Politics & Elections", "slug": "politics-elections" }
```

**Errors:** 400 (missing name/slug), 404 (not found), 409 (slug in use).

---

#### `DELETE /admin/categories/{id}`

Delete a category.

**Success:** `204 No Content` (empty body)

**Errors:** 404 (not found).

---

### Events — create

#### `POST /admin/events/binary`

Create a binary event with one initial market.

**On-chain prerequisite:** the admin wallet must call
`ConditionalTokens.prepareCondition(...)` on Polygon first to create the
condition with exactly 2 outcome slots. The backend verifies this read-only.

**Request body:**
```json
{
  "slug": "us-election-2026",
  "title": "US Election 2026",
  "description": "Who will win the 2026 US presidential election?",
  "category_id": "cat_politics",
  "end_date": "2026-11-04T00:00:00Z",
  "market": {
    "slug": "will-incumbent-win",
    "question": "Will the incumbent win?",
    "outcome_yes_label": "Yes",
    "outcome_no_label": "No",
    "token_id_yes": "7384738291...",
    "token_id_no": "4829103746...",
    "condition_id": "0xabc0000000000000000000000000000000000000000000000000000000000def",
    "question_id": "0x1230000000000000000000000000000000000000000000000000000000000456",
    "tick_size": "0.01",
    "min_size": 5,
    "max_size": null,
    "fee_rate_bps": null
  }
}
```

**Backend verification (read-only chain calls):**
1. `condition_id` and `question_id` are well-formed `0x`-prefixed 32-byte hex.
2. `token_id_yes` and `token_id_no` are canonical decimal uint256 strings, distinct.
3. `getOutcomeSlotCount(condition_id)` returns exactly 2.
4. Derived position IDs for the condition + collateral match `token_id_yes` /
   `token_id_no`.

**Success:** `201 Created`
```json
{
  "event": { "id": "evt_abc123", "slug": "us-election-2026", ... },
  "markets": [ { "id": "mkt_xyz789", ... } ]
}
```
The `markets` array contains exactly one market (the initial market). The event
and market are created in `PAUSED` status.

**Errors:**
- 400 — malformed JSON, missing required field, bad hex/token-id format, invalid tick_size.
- 409 — slug, condition_id, or question_id already in use.
- 422 — condition not prepared on-chain (outcome slot count 0), outcome slot count != 2, or token IDs don't match on-chain derivation.
- 502 — chain read failed (`on-chain read failed; please retry`), or DB write succeeded but config publish failed (`event created but config publish failed; please retry`).

---

#### `POST /admin/events/neg-risk`

Create a NegRisk event with one initial market.

**On-chain prerequisite:** the admin wallet must prepare the NegRisk market and
question on-chain (via the NegRisk adapter). The backend derives `condition_id`
server-side from the adapter — do **not** send `condition_id` in the request.

**Request body:**
```json
{
  "slug": "world-cup-2026-winner",
  "title": "World Cup 2026 Winner",
  "description": "Which team will win the 2026 FIFA World Cup?",
  "category_id": "cat_sports",
  "end_date": "2026-07-19T00:00:00Z",
  "neg_risk_market_id": "0x7890000000000000000000000000000000000000000000000000000000000000",
  "market": {
    "slug": "will-team-a-win",
    "question": "Will Team A win?",
    "outcome_yes_label": "Yes",
    "outcome_no_label": "No",
    "token_id_yes": "1928374650...",
    "token_id_no": "8273645019...",
    "question_id": "0x7890000000000000000000000000000000000000000000000000000000000001",
    "tick_size": "0.01",
    "min_size": 5,
    "max_size": null,
    "fee_rate_bps": null
  }
}
```

**Field rules for NegRisk:**
- `neg_risk_market_id` is required, must be `0x`-prefixed 32-byte hex with
  **final byte `00`**.
- `market.question_id` first 31 bytes must match `neg_risk_market_id`; the
  final byte is the question index (e.g. `01`, `02`, ...).
- `market.condition_id` is **not sent** — the backend derives it via the
  NegRisk adapter's `getConditionId(questionId)`.

**Backend verification:**
1. Derives `condition_id` from the adapter.
2. `getOutcomeSlotCount` returns exactly 2.
3. Derived NegRisk position IDs match `token_id_yes` / `token_id_no`.

**Success:** `201 Created` — same shape as binary create.

**Errors:**
- 400 — `neg_risk_market_id` missing, not hex, or final byte non-zero;
  `question_id` doesn't belong to `neg_risk_market_id` (first 31 bytes mismatch);
  other field validation.
- 409 — slug/question_id already in use.
- 422 — condition not prepared, slot count != 2, token IDs don't match.
- 502 — NegRisk adapter not configured on this deploy (`neg-risk adapter not configured on this deploy`), chain read failed, or publish failed after DB write.

---

### Events — append one market

#### `POST /admin/events/{id}/binary/markets`

Append one binary market to an existing binary event.

**On-chain prerequisite:** prepare a new condition on-chain (same as binary create).

**Request body:**
```json
{
  "market": {
    "slug": "will-challenger-win",
    "question": "Will the challenger win?",
    "outcome_yes_label": "Yes",
    "outcome_no_label": "No",
    "token_id_yes": "6271938405...",
    "token_id_no": "3840572619...",
    "condition_id": "0xdef0000000000000000000000000000000000000000000000000000000000abc",
    "question_id": "0x4560000000000000000000000000000000000000000000000000000000000789",
    "tick_size": "0.01",
    "min_size": 5
  }
}
```

**Preconditions:** the event must exist, be `BINARY`, and not be in a terminal
status.

**Success:** `200 OK`
```json
{
  "event": { ... },
  "markets": [ { ... }, { ... } ]
}
```
The `markets` array contains **all** markets in the event after the append
(both the existing ones and the newly added one).

**Errors:**
- 400 — field validation.
- 404 — event not found.
- 409 — event is terminal; slug/condition_id/question_id in use.
- 422 — event type is not `BINARY`; condition not prepared; token IDs mismatch.
- 502 — chain read or publish failure.

---

#### `POST /admin/events/{id}/neg-risk/markets`

Append one NegRisk market (question) to an existing NegRisk event.

**On-chain prerequisite:** prepare the question on the NegRisk adapter on-chain.

**Request body:**
```json
{
  "market": {
    "slug": "will-team-b-win",
    "question": "Will Team B win?",
    "outcome_yes_label": "Yes",
    "outcome_no_label": "No",
    "token_id_yes": "3847561029...",
    "token_id_no": "9201837465...",
    "question_id": "0x7890000000000000000000000000000000000000000000000000000000000002",
    "tick_size": "0.01",
    "min_size": 5
  }
}
```

The `question_id` first 31 bytes must match the event's `neg_risk_market_id`.
`condition_id` is not sent — derived server-side.

**Success:** `200 OK` — all markets in the event after append.

**Errors:** same as binary append, plus 422 if event type is not `NEG_RISK`.

---

### Events — update metadata

#### `PUT /admin/events/{id}`

Update event metadata. All fields are optional (pointer semantics — only
provided fields are updated).

**Request body:**
```json
{
  "title": "US Election 2026 (Updated)",
  "description": "Updated description.",
  "category_id": "cat_politics",
  "featured": true,
  "featured_sort_order": 1
}
```

**Success:** `200 OK` — the updated event object.

**Errors:** 400 (validation), 404 (not found).

---

### Events — resolve

#### `POST /admin/events/{id}/binary/resolve`

Resolve one or more markets in a binary event.

**On-chain prerequisite:** the admin wallet must call
`ConditionalTokens.reportPayouts(conditionId, [payouts])` on Polygon **before**
calling the backend. For a YES resolution, report `[1, 0]`; for NO, report
`[0, 1]`. The backend verifies the on-chain payouts match the declared outcome.

**Request body:**
```json
{
  "outcomes": {
    "mkt_xyz789": "YES"
  }
}
```

- Keys are market IDs (backend-generated, from the create/append response).
- Values are `"YES"` or `"NO"`.
- For binary markets, **YES = numerator index 0, NO = numerator index 1**.

**Backend verification:**
1. Each market ID belongs to the event.
2. `payoutDenominator(conditionId)` is non-zero (payouts were reported).
3. `payoutNumerators` match the declared outcome: YES → `[positive, 0]`,
   NO → `[0, positive]`.

**Success:** `200 OK`
```json
{
  "event": { ... },
  "markets": [ { ... "status": "RESOLVED", "outcome": "YES" } ]
}
```

**Errors:**
- 400 — `outcomes` empty or invalid value (not YES/NO).
- 404 — event not found.
- 422 — event type is not `BINARY`; market doesn't belong to event; payouts not
  reported (denominator 0); declared outcome contradicts on-chain numerators.
- 502 — chain read or publish failure.

---

#### `POST /admin/events/{id}/neg-risk/resolve`

Resolve one or more markets in a NegRisk event.

**On-chain prerequisite:** the admin wallet must report the outcome on the
NegRisk adapter on-chain.

**Request body:** same shape as binary resolve:
```json
{
  "outcomes": {
    "mkt_neg1": "YES"
  }
}
```

**NegRisk-specific behavior:** if another market in the same NegRisk event has
already resolved YES, the adapter marks the market as determined and the
question's payouts can never be reported. In this case the backend returns 422
with the message: `another market in this NegRisk event already resolved YES;
this question can never be reported`.

**Success:** `200 OK` — same shape as binary resolve.

**Errors:** same as binary resolve, plus the NegRisk dead-end message above.

---

### Events — void

#### `POST /admin/events/{id}/void`

Void active markets in a binary event.

**On-chain prerequisite:** the admin wallet must call `reportPayouts` with
**equal positive numerators** for YES and NO (e.g. `[1, 1]`). This is the void
pattern — it signals "refund everyone." The backend verifies this pattern
on-chain.

**Request body** (optional — may be omitted entirely):
```json
{
  "market_ids": ["mkt_xyz789", "mkt_abc456"]
}
```

- If `market_ids` is omitted or empty, the backend targets **every market
  currently in `ACTIVE` status** in the event.
- If provided, each ID must belong to the event.

**NegRisk:** voiding is **not supported**. Calling void on a NegRisk event
returns 400: `void_not_supported_for_negrisk: voiding NegRisk markets is not yet
supported`.

**Backend verification (binary only):**
1. `payoutDenominator` is non-zero.
2. `payoutNumerators` are both positive and equal (the void pattern).

**Success:** `200 OK`
```json
{
  "event": { ... },
  "markets": [ { ... "status": "VOIDED" } ]
}
```

**Errors:**
- 400 — NegRisk event (void unsupported).
- 404 — event not found.
- 422 — no markets to void (none active and none specified); a target market
  doesn't belong to the event; payouts not reported; payouts are not equal
  positive (decisive, not void pattern).
- 502 — chain read or publish failure.

---

### Markets

#### `POST /admin/markets/bulk-pause`

Pause multiple markets atomically. If any market is missing or not in `ACTIVE`
status, the entire batch is rejected — no partial state change.

**Request body:**
```json
{
  "market_ids": ["mkt_xyz789", "mkt_abc456"]
}
```

**Success:** `200 OK`
```json
{
  "markets": [ { ... "status": "PAUSED" }, { ... "status": "PAUSED" } ]
}
```

**Errors:** 400 (empty list), 404 (a market not found), 409 (a market not in
ACTIVE status), 502 (publish failure).

---

#### `GET /admin/markets/{id}`

Get a single market with a **live status overlay**.

The `status` field is overlaid from the market-config KV bucket (the state the
trading service actually operates on), not just the database record. This is
critical for detecting publish/retry states after a failed write:

- If the market's config has been published to KV, `status` reflects the live KV
  value.
- If the market has **never** been published to KV (e.g. the publish after
  create failed), `status` is `"UNPUBLISHED"`.

**Success:** `200 OK` — the market object (see [§5](#5-response-field-reference)).

**Errors:** 404 (not found), 502 (KV read failed — `reading live market config
failed; please retry`).

> **UI guidance:** use this endpoint to detect whether a write's downstream
> publish succeeded. After a 502 retry, `GET` the market: if `status` is
> `UNPUBLISHED` or doesn't match what you just wrote, retry the original write.

---

#### `PUT /admin/markets/{id}`

Update market metadata (question, outcome labels). All fields optional.

**Request body:**
```json
{
  "question": "Updated question text?",
  "outcome_yes_label": "Yes",
  "outcome_no_label": "No"
}
```

**Success:** `200 OK` — updated market object.

**Errors:** 400 (validation), 404 (not found), 502 (publish failure).

---

#### `POST /admin/markets/{id}/pause`

Pause a single market (transition to `PAUSED`).

**Success:** `200 OK` — updated market object.

**Errors:** 404 (not found), 409 (invalid transition — e.g. already paused,
resolved, or voided), 502 (publish failure).

---

#### `POST /admin/markets/{id}/resume`

Resume a single market (transition to `ACTIVE`).

**Success:** `200 OK` — updated market object.

**Errors:** 404 (not found), 409 (invalid transition), 502 (publish failure).

---

#### `PUT /admin/markets/{id}/fee-rate`

Set the per-market fee rate (basis points).

**Request body:**
```json
{
  "fee_rate_bps": 100
}
```

**Success:** `200 OK`
```json
{
  "market_id": "mkt_xyz789",
  "fee_rate_bps": 100,
  "updated_at": "2026-06-18T12:00:00Z"
}
```

**Errors:** 400 (invalid fee rate), 404 (not found), 502 (publish failure).

---

#### `PUT /admin/markets/{id}/trading-config`

Update the market's trading configuration (tick size, min/max size).

**Request body:**
```json
{
  "tick_size": "0.02",
  "min_size": 10,
  "max_size": 5000
}
```

**Success:** `200 OK` — updated market object.

**Errors:** 400 (invalid tick_size or config), 404 (not found), 502 (publish
failure).

---

## 7. Retry semantics

### The 502 `please retry` pattern

A 502 with a message ending in `please retry` means one of two things:

1. **Chain read failed** — the backend couldn't read from the Polygon RPC. The
   message is `on-chain read failed; please retry`. No DB write occurred.
   Retry the same call.

2. **DB write succeeded but downstream publish failed** — the market/event was
   persisted to PostgreSQL, but publishing the market config to NATS KV (or the
   status change to Core NATS) failed. The message describes what persisted,
   e.g. `event created but config publish failed; please retry`.

### What to do on 502

**Retry the exact same admin call.** Writes are designed to be idempotent
enough to republish:

- **Create:** if the event/market was already persisted (the DB write in the
  failed call), retrying will return a 409 (slug already in use). In that case,
  the record exists — use `GET /admin/markets/{id}` or the event's markets to
  find it, then check whether its config was published.
- **Append:** same as create — 409 on the slug if already persisted.
- **Resolve / void / pause / resume / fee-rate / trading-config:** these are
  state transitions. Retrying after a publish failure republishes the config.
  If the transition already fully succeeded, retrying is a no-op or returns 409
  (already in the target state) — both are safe.

### Detecting publish state with `GET /admin/markets/{id}`

After a 502 on a write, call `GET /admin/markets/{id}`:

- If `status` is `"UNPUBLISHED"` — the market's config never reached KV. Retry
  the original write to republish.
- If `status` matches what you just wrote — the publish eventually succeeded
  (or a previous retry did). No further action needed.
- If `status` is the pre-write value — the DB transition may not have
  committed. Retry the original write.

### Recovery flow example

```
1. POST /admin/events/binary  →  502 "event created but config publish failed; please retry"
2. GET  /admin/markets/{id}   →  200 { "status": "UNPUBLISHED", ... }
3. POST /admin/events/binary  →  409 "slug ... already in use"
   (the event exists; retrying create won't republish)
4. Use PUT /admin/markets/{id} (metadata update) or
   POST /admin/markets/{id}/resume → pause to trigger a config republish,
   or contact a backend operator to republish the KV entry.
```

> **Note:** the create/append endpoints return 409 on duplicate slug, which
> means you cannot simply re-call create to republish. For create/append
> publish failures, the safest recovery is to trigger a config republish via
> a market update endpoint (`PUT /admin/markets/{id}` or
> `POST /admin/markets/{id}/pause` then `resume`), which republishes the
> market config to KV on success.

---

## 8. Lifecycle examples

### 8.1 Binary lifecycle

```
Step 1: Create category
  POST /admin/categories
  → 201 { "id": "cat_pol", ... }

Step 2: Admin wallet prepares condition on-chain
  ConditionalTokens.prepareCondition(questionId, oracle, 2)
  → conditionId on Polygon

Step 3: Derive token IDs off-chain
  (Collateral + conditionId → ERC1155 position IDs for YES/NO)

Step 4: Create binary event with one market
  POST /admin/events/binary
  body: { slug, title, ..., market: { condition_id, question_id, token_id_yes, token_id_no, ... } }
  → 201 { "event": { "id": "evt_1", ... }, "markets": [ { "id": "mkt_1", "status": "PAUSED", ... } ] }

Step 5: Append another binary market
  POST /admin/events/evt_1/binary/markets
  body: { "market": { ...new condition... } }
  → 200 { "event": ..., "markets": [ mkt_1, mkt_2 ] }

Step 6: Resume markets (make them tradeable)
  POST /admin/markets/mkt_1/resume  → 200 { "status": "ACTIVE", ... }
  POST /admin/markets/mkt_2/resume  → 200 { "status": "ACTIVE", ... }

Step 7: Resolve one market
  [On-chain] ConditionalTokens.reportPayouts(conditionId_mkt_1, [1, 0])  // YES wins
  POST /admin/events/evt_1/binary/resolve
  body: { "outcomes": { "mkt_1": "YES" } }
  → 200 { "event": ..., "markets": [ { "id": "mkt_1", "status": "RESOLVED", "outcome": "YES" }, ... ] }

Step 8: Void remaining active market(s)
  [On-chain] ConditionalTokens.reportPayouts(conditionId_mkt_2, [1, 1])  // equal positive = void
  POST /admin/events/evt_1/void
  body: { "market_ids": ["mkt_2"] }
  → 200 { "event": ..., "markets": [ { "id": "mkt_2", "status": "VOIDED" } ] }
```

### 8.2 NegRisk lifecycle

```
Step 1: Admin wallet prepares NegRisk market + first question on-chain
  NegRiskAdapter.prepareMarket(negRiskMarketId)
  NegRiskAdapter.prepareQuestion(questionId_1, negRiskMarketId)

Step 2: Create NegRisk event with one market
  POST /admin/events/neg-risk
  body: { slug, title, ..., neg_risk_market_id: "0x...00", market: { question_id: "0x...01", ... } }
  → 201 { "event": { "neg_risk_market_id": "0x...00", ... }, "markets": [ { "id": "mkt_n1", ... } ] }

Step 3: Append another question/market
  [On-chain] NegRiskAdapter.prepareQuestion(questionId_2, negRiskMarketId)
  POST /admin/events/evt_n1/neg-risk/markets
  body: { "market": { "question_id": "0x...02", ... } }
  → 200 { "event": ..., "markets": [ mkt_n1, mkt_n2 ] }

Step 4: Resolve one market
  [On-chain] report outcome for questionId_1
  POST /admin/events/evt_n1/neg-risk/resolve
  body: { "outcomes": { "mkt_n1": "YES" } }
  → 200 { ..., "markets": [ { "status": "RESOLVED", "outcome": "YES" }, ... ] }

Step 5: Attempting to resolve a second market as YES
  → 422 "another market in this NegRisk event already resolved YES; this question can never be reported"
  (Once one question resolves YES, the adapter is determined; other questions cannot be reported.)

Void: NOT SUPPORTED for NegRisk events.
  POST /admin/events/evt_n1/void → 400 "void_not_supported_for_negrisk: ..."
```

### 8.3 Retry / recovery lifecycle

```
Step 1: Create binary event
  POST /admin/events/binary → 502 "event created but config publish failed; please retry"

Step 2: Check live status
  GET /admin/markets/{market_id_from_step_1_response}
  → 200 { "status": "UNPUBLISHED", ... }
  (DB write succeeded; KV publish did not)

Step 3: Retry create (same payload)
  POST /admin/events/binary → 409 "slug, condition_id, or question_id already in use"
  (Record exists — create can't republish)

Step 4: Trigger config republish via a market update
  PUT /admin/markets/{market_id} { "question": "same question" }
  → 200 { "status": "PAUSED", ... }
  (The update handler republishes market config to KV on success)

Step 5: Verify
  GET /admin/markets/{market_id}
  → 200 { "status": "PAUSED", ... }
  (No longer UNPUBLISHED — config is live)
```

---

## 9. Full route table

| Method | Path | Success | Auth |
|--------|------|---------|------|
| POST | `/admin/categories` | 201 `{id,name,slug}` | admin |
| PUT | `/admin/categories/{id}` | 200 `{id,name,slug}` | admin |
| DELETE | `/admin/categories/{id}` | 204 | admin |
| POST | `/admin/events/binary` | 201 `{event,markets}` | admin |
| POST | `/admin/events/neg-risk` | 201 `{event,markets}` | admin |
| POST | `/admin/events/{id}/binary/markets` | 200 `{event,markets}` | admin |
| POST | `/admin/events/{id}/neg-risk/markets` | 200 `{event,markets}` | admin |
| PUT | `/admin/events/{id}` | 200 `event` | admin |
| POST | `/admin/events/{id}/binary/resolve` | 200 `{event,markets}` | admin |
| POST | `/admin/events/{id}/neg-risk/resolve` | 200 `{event,markets}` | admin |
| POST | `/admin/events/{id}/void` | 200 `{event,markets}` | admin |
| POST | `/admin/markets/bulk-pause` | 200 `{markets}` | admin |
| GET | `/admin/markets/{id}` | 200 `market` | admin |
| PUT | `/admin/markets/{id}` | 200 `market` | admin |
| POST | `/admin/markets/{id}/pause` | 200 `market` | admin |
| POST | `/admin/markets/{id}/resume` | 200 `market` | admin |
| PUT | `/admin/markets/{id}/fee-rate` | 200 `{market_id,fee_rate_bps,updated_at}` | admin |
| PUT | `/admin/markets/{id}/trading-config` | 200 `market` | admin |

---

## 10. On-chain test harness reference

The backend's on-chain assumptions are verified by a forked-Polygon test
harness (`docs/testing-onchain.md`, `make test-onchain`). Key proofs relevant
to the admin panel:

- **Binary create:** condition prepared → 201; never prepared → 422; token IDs
  not matching on-chain derivation → 422.
- **Binary resolve:** `reportPayouts([1,0])` + declare YES → 200 (proves
  YES = index 0); payouts never reported → 422; declared outcome contradicts
  on-chain → 422.
- **Binary void:** `reportPayouts([1,1])` → 200; decisive payouts → 422.
- **NegRisk:** server-side `condition_id` derivation matches the adapter;
  token derivation cross-checks against raw CTHelpers math; first YES resolves
  200; second YES → 422 via `getDetermined` pre-check.
- **Transport failure:** dead RPC → 502, never a false accept.

These tests impersonate the production admin wallet via `anvil_impersonateAccount`
— no private keys exist in the repo, and the services keep zero on-chain write
capability. The admin wallet signs all chain transactions from the frontend;
the backend verifies read-only.

---

## Appendix: design context

- **Design decision #11** (`docs/rules/design-decisions.md`): URL-as-discriminator
  for type-varying admin endpoints. Each variant gets its own URL rather than a
  single endpoint with an in-body `event_type` field. This keeps each handler's
  request shape unambiguous and readable top-to-bottom without type-branching.
- **Polymarket compatibility** (`docs/rules/polymarket.md`): the CLOB/trading
  surface is Polymarket SDK-compatible. The admin surface is Shisa-specific and
  does not follow the Polymarket admin API.
- **Auth split** (design decision #7/#8): platform service owns session auth
  (JWT); trading service owns API-key auth (HMAC). The two are non-overlapping —
  no endpoint accepts both. Admin routes use JWT only.
