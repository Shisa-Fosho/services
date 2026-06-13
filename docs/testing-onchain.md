# On-Chain Test Harness (Forked Polygon)

Integration tests that run the platform service's admin market-lifecycle
endpoints against an **anvil fork of Polygon mainnet**, where our
production contracts exist at their real deployed addresses. They close
the gap left by the unit tests' in-memory chain fakes (issue #81): the
fakes prove handler logic against *assumed* chain responses; these tests
prove the assumptions against the *deployed* contracts.

## Running

```bash
make test-onchain    # requires Docker; ~30s end to end
```

The target starts a fresh dockerized anvil fork on port 8546, runs
`go test -tags=onchain ./internal/platform/market/`, and tears the fork
down. Configuration (all optional):

| Variable | Default | Purpose |
|---|---|---|
| `ONCHAIN_FORK_RPC` | `https://polygon-bor-rpc.publicnode.com` | Upstream Polygon RPC anvil forks from |
| `ONCHAIN_FORK_BLOCK` | latest | Pin the fork block (needs an archive upstream) |
| `ONCHAIN_PORT` | `8546` | Host port for the fork (8545 is the compose stack's anvil) |
| `ONCHAIN_RPC_URL` | `http://127.0.0.1:8546` | Where the tests dial (set by the make target) |

**The fork must be fresh per run.** Public Polygon RPCs serve state for
only ~128 recent blocks (~4 minutes); anvil lazily fetches storage from
the upstream at its fork block, so a stale fork starts failing reads
mid-run. The make target always starts a new fork, and the suite
finishes well inside the window. For deterministic pinned-block runs,
point `ONCHAIN_FORK_RPC` at an archive endpoint (e.g. Alchemy) and set
`ONCHAIN_FORK_BLOCK`.

## What it proves

State is set up by **impersonating the production admin wallet**
(`0xe62A…e4C`) via `anvil_impersonateAccount` — no private keys exist
anywhere in this repo, and the services keep zero write capability
(write helpers live in the test-only `internal/shared/eth/ethtest`
package; production chain access stays pure `eth_call`, matching the
design where the admin wallet signs from the frontend).

Covered, against `internal/platform/market/handler_onchain_test.go`:

1. **Binary create** — condition prepared on the real ConditionalTokens →
   201; never prepared → 422; token ids not matching the on-chain
   derivation → 422.
2. **Binary resolve** — `reportPayouts([1,0])` then declaring YES → 200,
   which is the live proof that **YES = outcome index 0** on the
   deployed contracts; payouts never reported (failed/missing admin tx)
   → 422; declared winner contradicting on-chain payouts → 422.
3. **Binary void** — `reportPayouts([1,1])` → 200; decisive payouts →
   422.
4. **NegRisk** — server-side `condition_id` derivation matches the
   adapter's salt derivation for freshly prepared questions; the
   adapter's `getPositionId` token derivation cross-checks against raw
   CTHelpers math on the CTF under wrapped collateral; first YES
   resolves 200; a **second YES is rejected 422** via the
   `getDetermined` pre-check (the adapter reverts
   `MarketAlreadyDetermined` on-chain, so the question's payouts can
   never be reported).
5. **Transport failure** — a reader bound to a codeless address (the
   analogue of a dead or misconfigured RPC) surfaces 502, never a false
   accept.

## Scope notes

- **NegRisk determination is covered** (issue #81 allowed scoping it
  out): `prepareMarket` on the deployed adapter is permissionless with
  oracle = caller, so the impersonated admin can drive
  `prepareQuestion`/`reportOutcome` end to end.
- **Update endpoints are out of scope** — event/market metadata, pause/
  resume, fee rate, and trading config have no corresponding on-chain
  action; there is nothing to verify on chain.
- **No CI wiring** — the suite depends on Docker plus a public RPC and
  is run locally (like `make test-integration`).
