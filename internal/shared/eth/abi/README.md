# Vendored contract ABIs

These JSON files are the input to `abigen`. The generated Go bindings live under `../gen/` and are **NOT** checked into the repo — they are produced by `make gen-contracts` (or transitively by `make build` / `make test` / `make lint`).

## Why vendor ABIs but not generated Go

The ABIs are small, stable, and the canonical input we want to version-control. The generated Go is large, regenerable, and bloats diffs.

## Source provenance

| File | Upstream |
|---|---|
| `ConditionalTokens.json` | `shisa-contracts/abi/ConditionalTokens.json` (Gnosis CTF v1.0.3) |
| `NegRiskAdapter.json` | `shisa-contracts/abi/NegRiskAdapter.json` (Polymarket/neg-risk-ctf-adapter@v2.0.0) |

## Refreshing

When the upstream contracts change:

```bash
# 1. Rebuild contracts in the contracts repo
cd ../../../../../shisa-contracts && forge build

# 2. Copy the regenerated ABIs into the services repo
cp abi/ConditionalTokens.json   ../shisa-services/internal/shared/eth/abi/
cp abi/NegRiskAdapter.json      ../shisa-services/internal/shared/eth/abi/

# 3. Regenerate Go bindings (also runs automatically as part of make build/test/lint)
cd ../shisa-services && make gen-contracts
```

## Adding a new contract

1. Drop its ABI JSON in this directory (`Foo.json`).
2. Add a `//go:generate abigen --abi abi/Foo.json --pkg foo --type Foo --out gen/foo/foo.go` directive to `../generate.go`.
3. Write a thin reader wrapper alongside `conditionaltokens.go` / `negrisk.go` that exposes the narrow read surface consumers actually need.
4. `make gen-contracts` to verify it generates and compiles.

## What NOT to do

- **Don't** hand-write ABI JSON literals as Go string constants. Always vendor the upstream JSON and let abigen produce the bindings.
- **Don't** commit `../gen/`. It is regenerable from these ABIs at any time.
- **Don't** import `../gen/...` packages from handler code. Import the thin reader wrappers (e.g. `eth.CTReader`) instead — they exist precisely to keep the handler surface narrow and testable.
