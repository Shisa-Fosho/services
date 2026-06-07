// Package eth — code generation hooks for the contract bindings.
//
// Running `go generate ./internal/shared/eth/...` re-runs abigen
// against the vendored ABI JSON under ./abi/. The output goes to
// ./gen/<contract>/, which is .gitignored — `make build`, `make test`
// and `make lint` each depend on `make gen-contracts` to regenerate it
// on a fresh clone, and `make tools` installs the abigen they require.
//
// Refreshing the ABIs themselves:
//
//	cd ../../../shisa-contracts && forge build
//	cp abi/<Contract>.json <services>/internal/shared/eth/abi/
//	go generate ./internal/shared/eth/...
//
// Bumping the version of the upstream contracts is the only reason
// to regenerate — the generated Go is otherwise frozen alongside the
// ABI snapshot it was produced from.

//go:generate abigen --abi abi/ConditionalTokens.json --pkg conditionaltokens --type ConditionalTokens --out gen/conditionaltokens/conditionaltokens.go
//go:generate abigen --abi abi/NegRiskAdapter.json --pkg negriskadapter --type NegRiskAdapter --out gen/negriskadapter/negriskadapter.go

package eth
