// Package ethtest drives on-chain state changes against a local anvil
// fork for integration tests: impersonated transaction submission via
// anvil's RPC surface (anvil_impersonateAccount + eth_sendTransaction,
// where anvil signs internally for impersonated accounts).
//
// This package is test support only. It holds no private keys and no
// signers, and service code must never import it — the services' chain
// access stays read-only (eth_call), matching production where the
// admin wallet signs from the frontend. Calldata is packed from the
// same abigen-generated ABI metadata the readers bind against, so the
// write surface used in tests cannot drift from the vendored ABIs.
package ethtest

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	conditionaltokens "github.com/Shisa-Fosho/services/internal/shared/eth/gen/conditionaltokens"
	negriskadapter "github.com/Shisa-Fosho/services/internal/shared/eth/gen/negriskadapter"
)

// Chain is a handle to a local anvil node (typically a fork of Polygon
// mainnet). All helpers fail the test on error rather than returning
// errors — setup failures are never an expected outcome.
type Chain struct {
	test      *testing.T
	rpcClient *rpc.Client

	// Eth is the standard read client over the same connection, for
	// constructing production readers against the fork.
	Eth *ethclient.Client
}

// Dial connects to the anvil node at rpcURL and pings it. Fails the
// test if the node is unreachable.
func Dial(test *testing.T, rpcURL string) *Chain {
	test.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rpcClient, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		test.Fatalf("dialing anvil at %s: %v", rpcURL, err)
	}
	ethClient := ethclient.NewClient(rpcClient)
	if _, err := ethClient.ChainID(ctx); err != nil {
		test.Fatalf("reading chain id from %s: %v", rpcURL, err)
	}
	test.Cleanup(rpcClient.Close)
	return &Chain{test: test, rpcClient: rpcClient, Eth: ethClient}
}

// Impersonate unlocks an arbitrary address for transaction submission
// (anvil_impersonateAccount) and funds it with 100 native tokens for
// gas. No private key is involved — anvil signs on the node side.
func (chain *Chain) Impersonate(ctx context.Context, account common.Address) {
	chain.test.Helper()
	if err := chain.rpcClient.CallContext(ctx, nil, "anvil_impersonateAccount", account); err != nil {
		chain.test.Fatalf("anvil_impersonateAccount %s: %v", account.Hex(), err)
	}
	balance := new(big.Int).Mul(big.NewInt(100), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	if err := chain.rpcClient.CallContext(ctx, nil, "anvil_setBalance", account, hexutil.EncodeBig(balance)); err != nil {
		chain.test.Fatalf("anvil_setBalance %s: %v", account.Hex(), err)
	}
}

// SendAs submits calldata to a contract from an impersonated account
// and waits for the receipt. Fails the test if the node rejects the
// transaction (including reverts surfaced during gas estimation) or
// the transaction fails on-chain.
func (chain *Chain) SendAs(ctx context.Context, from, contract common.Address, calldata []byte) *types.Receipt {
	chain.test.Helper()
	var txHash common.Hash
	tx := map[string]any{
		"from": from,
		"to":   contract,
		"data": hexutil.Bytes(calldata),
	}
	if err := chain.rpcClient.CallContext(ctx, &txHash, "eth_sendTransaction", tx); err != nil {
		chain.test.Fatalf("eth_sendTransaction from %s to %s: %v", from.Hex(), contract.Hex(), err)
	}
	receipt := chain.waitReceipt(ctx, txHash)
	if receipt.Status != types.ReceiptStatusSuccessful {
		chain.test.Fatalf("tx %s from %s to %s reverted on-chain", txHash.Hex(), from.Hex(), contract.Hex())
	}
	return receipt
}

// waitReceipt polls for the receipt — anvil auto-mines, so the first
// poll almost always succeeds.
func (chain *Chain) waitReceipt(ctx context.Context, txHash common.Hash) *types.Receipt {
	chain.test.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		receipt, err := chain.Eth.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt
		}
		if time.Now().After(deadline) {
			chain.test.Fatalf("waiting for receipt of %s: %v", txHash.Hex(), err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// --- calldata packers ----------------------------------------------------

// ctABI returns the parsed ConditionalTokens ABI from the abigen
// metadata.
func ctABI(test *testing.T) *abi.ABI {
	test.Helper()
	parsed, err := conditionaltokens.ConditionalTokensMetaData.GetAbi()
	if err != nil {
		test.Fatalf("parsing ConditionalTokens ABI: %v", err)
	}
	return parsed
}

func negRiskABI(test *testing.T) *abi.ABI {
	test.Helper()
	parsed, err := negriskadapter.NegRiskAdapterMetaData.GetAbi()
	if err != nil {
		test.Fatalf("parsing NegRiskAdapter ABI: %v", err)
	}
	return parsed
}

func pack(test *testing.T, contractABI *abi.ABI, method string, args ...any) []byte {
	test.Helper()
	calldata, err := contractABI.Pack(method, args...)
	if err != nil {
		test.Fatalf("packing %s: %v", method, err)
	}
	return calldata
}

// PrepareConditionData packs ConditionalTokens.prepareCondition(oracle,
// questionId, 2) — the binary-market create primitive.
func PrepareConditionData(test *testing.T, oracle common.Address, questionID common.Hash) []byte {
	return pack(test, ctABI(test), "prepareCondition", oracle, [32]byte(questionID), big.NewInt(2))
}

// ReportPayoutsData packs ConditionalTokens.reportPayouts(questionId,
// numerators) — the binary resolve/void primitive ([1,0] YES, [0,1] NO,
// [1,1] void).
func ReportPayoutsData(test *testing.T, questionID common.Hash, numerators []int64) []byte {
	payouts := make([]*big.Int, len(numerators))
	for idx, numerator := range numerators {
		payouts[idx] = big.NewInt(numerator)
	}
	return pack(test, ctABI(test), "reportPayouts", [32]byte(questionID), payouts)
}

// PrepareMarketData packs NegRiskAdapter.prepareMarket(feeBips,
// metadata). The caller becomes the market's oracle; the resulting
// marketId is read from the receipt via MarketIDFromReceipt.
func PrepareMarketData(test *testing.T, feeBips int64, metadata []byte) []byte {
	return pack(test, negRiskABI(test), "prepareMarket", big.NewInt(feeBips), metadata)
}

// PrepareQuestionData packs NegRiskAdapter.prepareQuestion(marketId,
// metadata). Oracle-only on-chain; the resulting questionId is the
// marketId with its final byte set to the question index.
func PrepareQuestionData(test *testing.T, marketID common.Hash, metadata []byte) []byte {
	return pack(test, negRiskABI(test), "prepareQuestion", [32]byte(marketID), metadata)
}

// ReportOutcomeData packs NegRiskAdapter.reportOutcome(questionId,
// outcome). Oracle-only on-chain; outcome=true flips the market's
// determined flag.
func ReportOutcomeData(test *testing.T, questionID common.Hash, outcome bool) []byte {
	return pack(test, negRiskABI(test), "reportOutcome", [32]byte(questionID), outcome)
}

// MarketIDFromReceipt extracts the marketId from the MarketPrepared
// event in a prepareMarket receipt.
func MarketIDFromReceipt(test *testing.T, receipt *types.Receipt, adapter common.Address) common.Hash {
	test.Helper()
	marketPreparedSig := negRiskABI(test).Events["MarketPrepared"].ID
	for _, logEntry := range receipt.Logs {
		if logEntry.Address == adapter && len(logEntry.Topics) >= 2 && logEntry.Topics[0] == marketPreparedSig {
			return logEntry.Topics[1]
		}
	}
	test.Fatalf("no MarketPrepared event from %s in receipt %s", adapter.Hex(), receipt.TxHash.Hex())
	return common.Hash{}
}

// QuestionID returns the NegRisk questionId for a marketId and question
// index: the marketId with its final byte set to the index
// (NegRiskIdLib layout, neg-risk-ctf-adapter v2.0.0).
func QuestionID(marketID common.Hash, index byte) common.Hash {
	marketID[31] = index
	return marketID
}
