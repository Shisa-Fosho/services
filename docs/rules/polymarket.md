# Polymarket Compatibility

Our REST API aims for compatibility with Polymarket's CLOB client SDKs. The
upstream SDK source is the **authoritative spec** — not general knowledge,
not prior implementations, not pattern-matching from similar systems.

## Pinned Reference Versions

| Repo | Pinned ref | Purpose |
|------|-----------|---------|
| `Polymarket/clob-client` | `v5.8.2` | TypeScript SDK — auth headers, request signing, REST client |
| `Polymarket/py-clob-client` | `v0.34.6` | Python SDK — cross-reference for TS |
| `Polymarket/clob-order-utils` | `main` | Order signing primitives (pin when stable) |
| `Polymarket/ctf-exchange` | `80cbf37` | Core CTFExchange contract — `Order` EIP-712 struct, fee enforcement, settlement |
| `Polymarket/neg-risk-ctf-adapter` | `v2.0.0` | Multi-outcome (NegRisk) settlement adapter |
| `Polymarket/exchange-fee-module` | `v2.0.0` | `FeeModule` + `NegRiskFeeModule` — admin-operated fee refund / withdrawal |
| `Polymarket/conditional-tokens-contracts` | `v1.0.3` | Conditional token minting, splitting, merging, redemption |
| `Polymarket/proxy-factories` | `master` | Poly Proxy / Gnosis Safe user-wallet factories (pin when stable) |

Bump these versions intentionally when you decide to upgrade compatibility
target. Do not drift — if planning work references a newer behavior, pin
higher first.

## SDK Verification Rule

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

## Why This Matters (Incident Log)

- **PR #48 (nonce tracker)**: first implementation added an in-memory nonce
  tracker keyed on a `POLY_NONCE` header for HMAC-signed requests. The actual
  clob-client L2 headers don't send `POLY_NONCE` — it's only used on L1
  (EIP-712) headers. Caught in review by fetching the SDK source; the entire
  nonce subsystem was removed. Rule exists to prevent recurrence.

## Contracts (Read-Only Reference)

ABIs consumed from the `contracts` repo via copy. No import dependency.
Contract source is pinned — see the pinned versions table above for authoritative
repo refs. Fetch via `gh api repos/Polymarket/<repo>/contents/<path>?ref=<pin>`.

- CTFExchange (`Polymarket/ctf-exchange`) — binary market settlement, per-order fee in EIP-712 payload
- NegRiskCTFAdapter (`Polymarket/neg-risk-ctf-adapter`) — multi-outcome market settlement
- ConditionalTokens (`Polymarket/conditional-tokens-contracts`) — token minting, splitting, merging, redemption
- Fee Module (`Polymarket/exchange-fee-module`) — admin-operated fee refund / withdrawal (rates live per-order, not here)
- Proxy Factories (`Polymarket/proxy-factories`) — user wallet deployment

## Fee Model (Confirmed Against Pinned Contracts)

- **Fees are per-order, not global.** The `Order` EIP-712 struct in
  `Polymarket/ctf-exchange` (`src/exchange/libraries/OrderStructs.sol`)
  includes a `feeRateBps` field; users consent to the rate by signing it.
- **Hard on-chain cap is `MAX_FEE_RATE_BIPS = 1000` (10%)**, a `pure`
  constant in `src/exchange/mixins/Fees.sol`. Not admin-settable —
  changing it requires a contract upgrade. Backend validators must not
  allow signing orders above this, or the exchange will revert.
- **Polymarket's REST API exposes `GET /fee-rate?token_id=X`** returning
  a `base_fee` per market (see `clob-client/src/client.ts`
  `getFeeRateBps(tokenID)`). Rates are per-token, not a single global value.
- **No maker/taker split at the protocol level.** Each order carries a
  single `feeRateBps`. Operator policy decides what rate to stamp onto
  maker vs taker orders as they are built.
- **FeeModule is not a rate source** — it's an admin-operated refund /
  withdrawal utility that sits next to the exchange.
