package eth

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	negriskadapter "github.com/Shisa-Fosho/services/internal/shared/eth/gen/negriskadapter"
)

// NegRiskReader is the narrow read surface over Polymarket's
// NegRiskAdapter contract. Per the pattern in conditionaltokens.go,
// service handlers depend on this shared interface directly.
type NegRiskReader interface {
	// ConditionID returns the deterministic CT conditionId the
	// adapter would derive for the given questionId. The caller
	// must then read OutcomeSlotCount on the result to confirm
	// the question was actually prepared — this method is pure
	// derivation.
	ConditionID(ctx context.Context, questionID common.Hash) (common.Hash, error)

	// MarketDetermined returns true once any constituent question
	// in the given NegRiskAdapter market has been resolved YES —
	// the adapter's mutual-exclusivity flag.
	MarketDetermined(ctx context.Context, marketID common.Hash) (bool, error)

	// PositionIDs returns the ERC1155 position ids (YES, NO) the
	// adapter derives for a questionId: getPositionId(questionId,
	// true) is YES, false is NO (NegRiskAdapter v2.0.0). The adapter
	// derives against its wrapped collateral internally, so no
	// collateral parameter is needed.
	PositionIDs(ctx context.Context, questionID common.Hash) (yes, no *big.Int, err error)
}

// NegRiskReaderClient implements NegRiskReader against a live RPC
// endpoint via the generated bindings under gen/negriskadapter.
//
// A nil receiver represents "NegRisk is not configured on this
// deploy" — every method returns ErrNegRiskDisabled. This lets
// service main.go pass nil through to handlers that only optionally
// touch NegRisk, instead of needing per-service feature flags.
type NegRiskReaderClient struct {
	contract *negriskadapter.NegRiskAdapterCaller
}

// NewNegRiskReader binds the NegRiskAdapter contract at the given
// address. Pass the zero address to disable NegRisk on this client
// (the returned reader's methods all return ErrNegRiskDisabled).
func NewNegRiskReader(client *ethclient.Client, address common.Address) (*NegRiskReaderClient, error) {
	if address == (common.Address{}) {
		return &NegRiskReaderClient{contract: nil}, nil
	}
	caller, err := negriskadapter.NewNegRiskAdapterCaller(address, client)
	if err != nil {
		return nil, fmt.Errorf("binding NegRiskAdapter at %s: %w", address.Hex(), err)
	}
	return &NegRiskReaderClient{contract: caller}, nil
}

// ConditionID reads NegRiskAdapter.getConditionId(questionId).
func (reader *NegRiskReaderClient) ConditionID(ctx context.Context, questionID common.Hash) (common.Hash, error) {
	if reader.contract == nil {
		return common.Hash{}, ErrNegRiskDisabled
	}
	value, err := reader.contract.GetConditionId(&bind.CallOpts{Context: ctx}, questionID)
	if err != nil {
		return common.Hash{}, fmt.Errorf("calling NegRiskAdapter.getConditionId: %w", err)
	}
	return value, nil
}

// MarketDetermined reads NegRiskAdapter.getDetermined(marketId).
func (reader *NegRiskReaderClient) MarketDetermined(ctx context.Context, marketID common.Hash) (bool, error) {
	if reader.contract == nil {
		return false, ErrNegRiskDisabled
	}
	value, err := reader.contract.GetDetermined(&bind.CallOpts{Context: ctx}, marketID)
	if err != nil {
		return false, fmt.Errorf("calling NegRiskAdapter.getDetermined: %w", err)
	}
	return value, nil
}

// PositionIDs reads NegRiskAdapter.getPositionId(questionId, outcome)
// for outcome = true (YES) and false (NO).
func (reader *NegRiskReaderClient) PositionIDs(ctx context.Context, questionID common.Hash) (*big.Int, *big.Int, error) {
	if reader.contract == nil {
		return nil, nil, ErrNegRiskDisabled
	}
	yes, err := reader.contract.GetPositionId(&bind.CallOpts{Context: ctx}, questionID, true)
	if err != nil {
		return nil, nil, fmt.Errorf("calling NegRiskAdapter.getPositionId(yes): %w", err)
	}
	no, err := reader.contract.GetPositionId(&bind.CallOpts{Context: ctx}, questionID, false)
	if err != nil {
		return nil, nil, fmt.Errorf("calling NegRiskAdapter.getPositionId(no): %w", err)
	}
	return yes, no, nil
}
