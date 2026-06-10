//go:build onchain

// On-chain integration tests: the real admin endpoints wired to real
// eth readers, against an anvil fork of Polygon mainnet where our
// production contracts live at their deployed addresses. State is set
// up by impersonating the production admin wallet (ethtest) — no
// private keys — and every scenario asserts the handler's actual HTTP
// response, both accept and reject paths.
//
// Run via `make test-onchain`, which starts the fork and sets
// ONCHAIN_RPC_URL. See docs/testing-onchain.md.
package market

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/eth/ethtest"
)

// Production Polygon deployment (see issue #81). The anvil fork places
// these contracts at their real addresses.
var (
	onchainCT          = common.HexToAddress("0xa2E381988F264A01C0Ec75820482b8E2F2Ed256C")
	onchainNegRisk     = common.HexToAddress("0xD7bfaa4b9B15d91ef9c872745E791cdF35adAe62")
	onchainUSDC        = common.HexToAddress("0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359")
	onchainWrappedCol  = common.HexToAddress("0x7F10687A14B9B4318d04B46D278e6366E8d27418")
	onchainAdminWallet = common.HexToAddress("0xe62A4B251d4e8fac52B08Cc2b88F091548426e4C")
)

func onchainRPCURL() string {
	if url := os.Getenv("ONCHAIN_RPC_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:8546"
}

// onchainEnv bundles the fork handle with a mux whose handler uses the
// production readers (not fakes) and in-memory repo/publisher doubles.
type onchainEnv struct {
	chain   *ethtest.Chain
	mux     *http.ServeMux
	repo    *fakeRepo
	pub     *fakePublisher
	ct      *eth.CTReaderClient
	negRisk *eth.NegRiskReaderClient
	catID   string
}

func newOnchainEnv(test *testing.T) *onchainEnv {
	test.Helper()
	chain := ethtest.Dial(test, onchainRPCURL())
	ctReader, err := eth.NewCTReader(chain.Eth, onchainCT)
	if err != nil {
		test.Fatalf("binding CTReader: %v", err)
	}
	negRiskReader, err := eth.NewNegRiskReader(chain.Eth, onchainNegRisk)
	if err != nil {
		test.Fatalf("binding NegRiskReader: %v", err)
	}
	repo := newFakeRepo()
	pub := &fakePublisher{}
	handler := NewHandler(repo, pub, ctReader, negRiskReader, onchainUSDC, zap.NewNop())
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux, passThroughAdmin)
	chain.Impersonate(context.Background(), onchainAdminWallet)
	return &onchainEnv{
		chain: chain, mux: mux, repo: repo, pub: pub,
		ct: ctReader, negRisk: negRiskReader,
		catID: seedCatID(test, repo, "onchain-"+randomHash().Hex()[2:10]),
	}
}

// randomHash returns a unique bytes32, so repeated runs against the
// same long-lived fork never collide on already-prepared conditions.
func randomHash() common.Hash {
	var out common.Hash
	if _, err := rand.Read(out[:]); err != nil {
		panic(err)
	}
	return out
}

// binaryConditionID mirrors CTHelpers.getConditionId for the binary
// case: keccak256(oracle ‖ questionId ‖ uint256(2)). The value is
// cross-validated on-chain — prepareCondition + the handler's
// getOutcomeSlotCount check only line up if this derivation is right.
func binaryConditionID(oracle common.Address, questionID common.Hash) common.Hash {
	var slotCount common.Hash
	slotCount[31] = 2
	return crypto.Keccak256Hash(oracle.Bytes(), questionID.Bytes(), slotCount.Bytes())
}

// prepareBinaryCondition prepares a fresh condition on the forked
// ConditionalTokens with the production admin wallet as oracle and
// returns (questionID, conditionID).
func prepareBinaryCondition(test *testing.T, env *onchainEnv) (common.Hash, common.Hash) {
	test.Helper()
	questionID := randomHash()
	env.chain.SendAs(context.Background(), onchainAdminWallet, onchainCT,
		ethtest.PrepareConditionData(test, onchainAdminWallet, questionID))
	return questionID, binaryConditionID(onchainAdminWallet, questionID)
}

// reportBinaryPayouts reports payouts for a prepared condition as the
// admin oracle ([1,0] = YES, [0,1] = NO, [1,1] = void).
func reportBinaryPayouts(test *testing.T, env *onchainEnv, questionID common.Hash, numerators []int64) {
	test.Helper()
	env.chain.SendAs(context.Background(), onchainAdminWallet, onchainCT,
		ethtest.ReportPayoutsData(test, questionID, numerators))
}

// binaryCreateBody builds a one-market binary create request with token
// ids derived from the chain itself via the production reader.
func binaryCreateBody(test *testing.T, env *onchainEnv, slug string, conditionID, questionID common.Hash) []byte {
	test.Helper()
	yes, no, err := env.ct.PositionIDs(context.Background(), onchainUSDC, conditionID)
	if err != nil {
		test.Fatalf("deriving token ids: %v", err)
	}
	return binaryEventBodyWithTokens(slug, env.catID, conditionID.Hex(), questionID.Hex(), yes.String(), no.String())
}

// createBinaryEventOnchain drives the create endpoint and returns the
// created event + market ids.
func createBinaryEventOnchain(test *testing.T, env *onchainEnv, slug string, conditionID, questionID common.Hash) eventWithMarketsResponse {
	test.Helper()
	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/binary",
		binaryCreateBody(test, env, slug, conditionID, questionID))
	if rec.Code != http.StatusCreated {
		test.Fatalf("create: status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	var resp eventWithMarketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		test.Fatalf("decode create response: %v", err)
	}
	if len(resp.Markets) != 1 {
		test.Fatalf("markets in create response = %d, want 1", len(resp.Markets))
	}
	return resp
}

// --- binary create --------------------------------------------------------

func TestOnchain_CreateBinary_HappyPath(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-create-"+questionID.Hex()[2:10], conditionID, questionID)
	if created.Markets[0].ConditionID != conditionID.Hex() {
		test.Errorf("stored condition_id = %s, want %s", created.Markets[0].ConditionID, conditionID.Hex())
	}
}

func TestOnchain_CreateBinary_ConditionNotPrepared(test *testing.T) {
	env := newOnchainEnv(test)
	questionID := randomHash()
	conditionID := binaryConditionID(onchainAdminWallet, questionID)

	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/binary",
		binaryEventBodyWithTokens("oc-unprep-"+questionID.Hex()[2:10], env.catID,
			conditionID.Hex(), questionID.Hex(), "1", "2"))
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

func TestOnchain_CreateBinary_TokenIDMismatch(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)

	// Prepared condition, but token ids that don't match the on-chain
	// derivation for it.
	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/binary",
		binaryEventBodyWithTokens("oc-tokmis-"+questionID.Hex()[2:10], env.catID,
			conditionID.Hex(), questionID.Hex(), "12345", "67890"))
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

// --- binary resolve -------------------------------------------------------

func TestOnchain_ResolveBinary_YesReported(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-resolve-"+questionID.Hex()[2:10], conditionID, questionID)

	// Admin resolves YES on-chain: numerators [1,0]. Declaring YES must
	// pass — this is the live proof that YES = outcome index 0 on the
	// deployed contracts, not just in our fakes.
	reportBinaryPayouts(test, env, questionID, []int64{1, 0})

	body := []byte(`{"outcomes":{"` + created.Markets[0].ID + `":"YES"}}`)
	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", body)
	if rec.Code != http.StatusOK {
		test.Fatalf("resolve: status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
}

func TestOnchain_ResolveBinary_PayoutsNotReported(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-noreport-"+questionID.Hex()[2:10], conditionID, questionID)

	// No reportPayouts tx — e.g. the admin's resolve tx failed or was
	// never sent. The backend must reject the declaration.
	body := []byte(`{"outcomes":{"` + created.Markets[0].ID + `":"YES"}}`)
	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", body)
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

func TestOnchain_ResolveBinary_DeclaredWinnerContradictsChain(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-wrongwin-"+questionID.Hex()[2:10], conditionID, questionID)

	// Chain says NO won ([0,1]); admin declares YES.
	reportBinaryPayouts(test, env, questionID, []int64{0, 1})

	body := []byte(`{"outcomes":{"` + created.Markets[0].ID + `":"YES"}}`)
	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", body)
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

// --- binary void ----------------------------------------------------------

func TestOnchain_VoidBinary_EqualPayouts(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-void-"+questionID.Hex()[2:10], conditionID, questionID)

	// The binary void pattern: equal positive numerators [1,1].
	reportBinaryPayouts(test, env, questionID, []int64{1, 1})

	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", []byte(`{}`))
	if rec.Code != http.StatusOK {
		test.Fatalf("void: status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
}

func TestOnchain_VoidBinary_DecisivePayoutsRejected(test *testing.T) {
	env := newOnchainEnv(test)
	questionID, conditionID := prepareBinaryCondition(test, env)
	created := createBinaryEventOnchain(test, env, "oc-voidbad-"+questionID.Hex()[2:10], conditionID, questionID)

	// Chain shows a decisive YES — voiding must be rejected.
	reportBinaryPayouts(test, env, questionID, []int64{1, 0})

	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", []byte(`{}`))
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

// --- NegRisk ---------------------------------------------------------------

// prepareNegRiskMarket prepares a fresh adapter market (admin wallet as
// oracle) with two questions, returning (marketID, questionIDs).
func prepareNegRiskMarket(test *testing.T, env *onchainEnv) (common.Hash, [2]common.Hash) {
	test.Helper()
	ctx := context.Background()
	metadata := randomHash() // unique metadata → unique marketId per run
	receipt := env.chain.SendAs(ctx, onchainAdminWallet, onchainNegRisk,
		ethtest.PrepareMarketData(test, 0, metadata.Bytes()))
	marketID := ethtest.MarketIDFromReceipt(test, receipt, onchainNegRisk)
	env.chain.SendAs(ctx, onchainAdminWallet, onchainNegRisk,
		ethtest.PrepareQuestionData(test, marketID, []byte("q0")))
	env.chain.SendAs(ctx, onchainAdminWallet, onchainNegRisk,
		ethtest.PrepareQuestionData(test, marketID, []byte("q1")))
	return marketID, [2]common.Hash{ethtest.QuestionID(marketID, 0), ethtest.QuestionID(marketID, 1)}
}

// negRiskCreateBody builds the two-market create request with token ids
// derived through the adapter.
func negRiskCreateBody(test *testing.T, env *onchainEnv, slug string, marketID common.Hash, questionIDs [2]common.Hash) []byte {
	test.Helper()
	markets := ""
	for idx, questionID := range questionIDs {
		yes, no, err := env.negRisk.PositionIDs(context.Background(), questionID)
		if err != nil {
			test.Fatalf("deriving neg-risk token ids: %v", err)
		}
		if idx > 0 {
			markets += ","
		}
		markets += `{"slug":"` + slug + `-m` + questionID.Hex()[64:] + `","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			"token_id_yes":"` + yes.String() + `","token_id_no":"` + no.String() + `",
			"question_id":"` + questionID.Hex() + `","tick_size":"0.01","min_size":5}`
	}
	return []byte(`{
		"slug":"` + slug + `","title":"T","description":"D",
		"category_id":"` + env.catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + marketID.Hex() + `",
		"markets":[` + markets + `]
	}`)
}

func TestOnchain_CreateNegRisk_DerivationsMatchDeployedContracts(test *testing.T) {
	env := newOnchainEnv(test)
	ctx := context.Background()
	marketID, questionIDs := prepareNegRiskMarket(test, env)

	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/neg-risk",
		negRiskCreateBody(test, env, "oc-neg-"+marketID.Hex()[2:10], marketID, questionIDs))
	if rec.Code != http.StatusCreated {
		test.Fatalf("create: status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		test.Fatalf("decode: %v", err)
	}

	// The server-side condition_id derivation (adapter salt) must match
	// what the adapter actually prepared on the CTF.
	for idx, market := range created.Markets {
		derived, err := env.negRisk.ConditionID(ctx, questionIDs[idx])
		if err != nil {
			test.Fatalf("getConditionId: %v", err)
		}
		if market.ConditionID != derived.Hex() {
			test.Errorf("markets[%d].condition_id = %s, want adapter-derived %s", idx, market.ConditionID, derived.Hex())
		}
		// Cross-check the two independent token derivations: the
		// adapter's getPositionId(questionId, outcome) against raw
		// CTHelpers math on the CTF under wrapped collateral. Equality
		// proves both readers agree with the deployed contracts.
		adapterYes, adapterNo, err := env.negRisk.PositionIDs(ctx, questionIDs[idx])
		if err != nil {
			test.Fatalf("adapter PositionIDs: %v", err)
		}
		ctYes, ctNo, err := env.ct.PositionIDs(ctx, onchainWrappedCol, derived)
		if err != nil {
			test.Fatalf("ct PositionIDs: %v", err)
		}
		if adapterYes.Cmp(ctYes) != 0 || adapterNo.Cmp(ctNo) != 0 {
			test.Errorf("markets[%d]: adapter tokens (%s,%s) != CTF tokens (%s,%s)",
				idx, adapterYes, adapterNo, ctYes, ctNo)
		}
	}
}

func TestOnchain_ResolveNegRisk_SecondYesRejected(test *testing.T) {
	env := newOnchainEnv(test)
	ctx := context.Background()
	marketID, questionIDs := prepareNegRiskMarket(test, env)

	rec := doRequest(test, env.mux, http.MethodPost, "/admin/events/neg-risk",
		negRiskCreateBody(test, env, "oc-negres-"+marketID.Hex()[2:10], marketID, questionIDs))
	if rec.Code != http.StatusCreated {
		test.Fatalf("create: status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		test.Fatalf("decode: %v", err)
	}

	// Oracle reports question 0 YES on the adapter → payouts [1,0] land
	// on the CTF and the market flips to determined.
	env.chain.SendAs(ctx, onchainAdminWallet, onchainNegRisk,
		ethtest.ReportOutcomeData(test, questionIDs[0], true))

	marketByQuestion := map[string]string{}
	for _, market := range created.Markets {
		marketByQuestion[market.QuestionID] = market.ID
	}

	// Resolving the reported question succeeds.
	body := []byte(`{"outcomes":{"` + marketByQuestion[questionIDs[0].Hex()] + `":"YES"}}`)
	rec = doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/neg-risk/resolve", body)
	if rec.Code != http.StatusOK {
		test.Fatalf("first resolve: status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}

	// Declaring a second YES must be rejected: its payouts were never
	// reported, and the determined pre-check explains why.
	body = []byte(`{"outcomes":{"` + marketByQuestion[questionIDs[1].Hex()] + `":"YES"}}`)
	rec = doRequest(test, env.mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/neg-risk/resolve", body)
	if rec.Code != http.StatusUnprocessableEntity {
		test.Errorf("second resolve: status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

// --- transport failure ------------------------------------------------------

func TestOnchain_ChainReadFailure_Returns502(test *testing.T) {
	env := newOnchainEnv(test)

	// A reader bound to an address with no code: eth_call returns empty
	// data, the binding fails to unpack, and the handler must surface a
	// 502 — the analogue of a dead or misconfigured RPC.
	codeless, err := eth.NewCTReader(env.chain.Eth, common.HexToAddress("0x00000000000000000000000000000000000000AA"))
	if err != nil {
		test.Fatalf("binding codeless reader: %v", err)
	}
	negRiskReader, err := eth.NewNegRiskReader(env.chain.Eth, onchainNegRisk)
	if err != nil {
		test.Fatalf("binding NegRiskReader: %v", err)
	}
	handler := NewHandler(env.repo, env.pub, codeless, negRiskReader, onchainUSDC, zap.NewNop())
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux, passThroughAdmin)

	questionID := randomHash()
	conditionID := binaryConditionID(onchainAdminWallet, questionID)
	rec := doRequest(test, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBodyWithTokens("oc-502-"+questionID.Hex()[2:10], env.catID,
			conditionID.Hex(), questionID.Hex(), "1", "2"))
	if rec.Code != http.StatusBadGateway {
		test.Errorf("status = %d body=%q, want 502", rec.Code, rec.Body.String())
	}
}
