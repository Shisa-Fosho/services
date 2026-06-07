// On-chain read primitives shared across services.
//
// This file owns the RPC bootstrap and the sentinel errors that the
// per-contract readers (CTReader, NegRiskReader, …) reuse. Per-contract
// reader interfaces live in their own files and are composed by service
// main.go's into whatever subset that service needs.

package eth

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Sentinel errors. Handlers use errors.Is to map these to HTTP status
// codes — typically 422 for the "chain says no" cases, 502 for transport.
var (
	// ErrConditionNotPrepared is returned when getOutcomeSlotCount returns
	// 0, meaning the conditionId has never been prepared on
	// ConditionalTokens (or, for NegRisk, no question was prepared on the
	// adapter under the derived conditionId).
	ErrConditionNotPrepared = errors.New("condition not prepared on-chain")

	// ErrPayoutsNotReported is returned when payoutDenominator is zero,
	// i.e. the oracle has not called reportPayouts yet for the condition.
	ErrPayoutsNotReported = errors.New("payouts not reported on-chain")

	// ErrNegRiskDisabled is returned when a NegRisk-only method is called
	// against a client constructed with the zero address for the
	// NegRiskAdapter (deploys without NegRisk support).
	ErrNegRiskDisabled = errors.New("neg-risk adapter is not configured")
)

// Dial opens an ethclient connection to the RPC URL and pings it once
// for the chain ID — fail-fast on bad config at boot. Callers own the
// returned client and must Close it when done.
func Dial(ctx context.Context, rpcURL string) (*ethclient.Client, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dialing rpc %q: %w", rpcURL, err)
	}
	if _, err := client.ChainID(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("reading chain id: %w", err)
	}
	return client, nil
}
