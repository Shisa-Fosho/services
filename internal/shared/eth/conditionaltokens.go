package eth

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	conditionaltokens "github.com/Shisa-Fosho/services/internal/shared/eth/gen/conditionaltokens"
)

// CTReader is the narrow read surface over the Gnosis ConditionalTokens
// contract. Service handlers depend on this shared interface directly;
// only declare a local subset when a consumer genuinely uses a strict
// subset of these methods (see docs/rules/conventions.md, "On-chain
// Contract Bindings"). The concrete *CTReaderClient below is the
// production implementation.
type CTReader interface {
	// OutcomeSlotCount returns the prepared outcome count for a
	// conditionId. Zero means "never prepared".
	OutcomeSlotCount(ctx context.Context, conditionID common.Hash) (uint64, error)

	// PayoutDenominator returns the denominator for a resolved
	// condition. Zero means "payouts not reported yet".
	PayoutDenominator(ctx context.Context, conditionID common.Hash) (*big.Int, error)

	// PayoutNumerators returns one numerator per outcome slot, in
	// order. By Polymarket convention for binary markets index 0 =
	// YES, index 1 = NO. The conditional-tokens contract exposes
	// the array as an indexed getter, so this method makes slotCount
	// RPC calls — fine for the binary (N=2) hot path.
	PayoutNumerators(ctx context.Context, conditionID common.Hash, slotCount uint64) ([]*big.Int, error)

	// PositionIDs returns the ERC1155 position ids (YES, NO) for a
	// binary condition under the given collateral token: per CTHelpers,
	// positionId = getPositionId(collateral, getCollectionId(0,
	// conditionId, indexSet)) with indexSet 1 (0b01) = YES and 2
	// (0b10) = NO. getCollectionId is a contract call (view, EC-point
	// hashing), so derivation goes through the chain rather than being
	// reimplemented here.
	PositionIDs(ctx context.Context, collateralToken common.Address, conditionID common.Hash) (yes, no *big.Int, err error)
}

// CTReaderClient implements CTReader against a live RPC endpoint via
// the generated bindings under gen/conditionaltokens.
type CTReaderClient struct {
	contract *conditionaltokens.ConditionalTokensCaller
}

// NewCTReader binds the conditional-tokens contract at the given
// address to the supplied RPC client. The contract's existence at
// that address is NOT verified here (an empty account silently
// returns zero on every read) — callers can detect missing deploys
// by treating "OutcomeSlotCount returned 0 for a condition we
// expected to be prepared" as ErrConditionNotPrepared.
func NewCTReader(client *ethclient.Client, address common.Address) (*CTReaderClient, error) {
	caller, err := conditionaltokens.NewConditionalTokensCaller(address, client)
	if err != nil {
		return nil, fmt.Errorf("binding ConditionalTokens at %s: %w", address.Hex(), err)
	}
	return &CTReaderClient{contract: caller}, nil
}

// OutcomeSlotCount reads getOutcomeSlotCount(conditionId).
func (reader *CTReaderClient) OutcomeSlotCount(ctx context.Context, conditionID common.Hash) (uint64, error) {
	count, err := reader.contract.GetOutcomeSlotCount(&bind.CallOpts{Context: ctx}, conditionID)
	if err != nil {
		return 0, fmt.Errorf("calling getOutcomeSlotCount: %w", err)
	}
	if !count.IsUint64() {
		return 0, fmt.Errorf("getOutcomeSlotCount: value %s exceeds uint64", count.String())
	}
	return count.Uint64(), nil
}

// PayoutDenominator reads payoutDenominator(conditionId).
func (reader *CTReaderClient) PayoutDenominator(ctx context.Context, conditionID common.Hash) (*big.Int, error) {
	value, err := reader.contract.PayoutDenominator(&bind.CallOpts{Context: ctx}, conditionID)
	if err != nil {
		return nil, fmt.Errorf("calling payoutDenominator: %w", err)
	}
	return value, nil
}

// PayoutNumerators reads payoutNumerators(conditionId, idx) for each
// index in [0, slotCount).
func (reader *CTReaderClient) PayoutNumerators(ctx context.Context, conditionID common.Hash, slotCount uint64) ([]*big.Int, error) {
	out := make([]*big.Int, slotCount)
	for idx := uint64(0); idx < slotCount; idx++ {
		value, err := reader.contract.PayoutNumerators(&bind.CallOpts{Context: ctx}, conditionID, new(big.Int).SetUint64(idx))
		if err != nil {
			return nil, fmt.Errorf("calling payoutNumerators[%d]: %w", idx, err)
		}
		out[idx] = value
	}
	return out, nil
}

// PositionIDs derives the (YES, NO) position ids for a binary condition
// via getCollectionId + getPositionId.
func (reader *CTReaderClient) PositionIDs(ctx context.Context, collateralToken common.Address, conditionID common.Hash) (*big.Int, *big.Int, error) {
	yes, err := reader.positionID(ctx, collateralToken, conditionID, indexSetYes)
	if err != nil {
		return nil, nil, err
	}
	no, err := reader.positionID(ctx, collateralToken, conditionID, indexSetNo)
	if err != nil {
		return nil, nil, err
	}
	return yes, no, nil
}

// Index sets for the binary partition [0b01, 0b10] — YES is outcome
// slot 0, NO is slot 1 (Polymarket convention).
const (
	indexSetYes = 1
	indexSetNo  = 2
)

func (reader *CTReaderClient) positionID(ctx context.Context, collateralToken common.Address, conditionID common.Hash, indexSet int64) (*big.Int, error) {
	collectionID, err := reader.contract.GetCollectionId(&bind.CallOpts{Context: ctx},
		common.Hash{}, conditionID, big.NewInt(indexSet))
	if err != nil {
		return nil, fmt.Errorf("calling getCollectionId(indexSet=%d): %w", indexSet, err)
	}
	positionID, err := reader.contract.GetPositionId(&bind.CallOpts{Context: ctx},
		collateralToken, collectionID)
	if err != nil {
		return nil, fmt.Errorf("calling getPositionId(indexSet=%d): %w", indexSet, err)
	}
	return positionID, nil
}
