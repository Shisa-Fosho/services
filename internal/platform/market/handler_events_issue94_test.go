package market

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Shisa-Fosho/services/internal/shared/testassert"
	"github.com/ethereum/go-ethereum/common"
)

func binaryEventSingleMarketBody(slug, categoryID, conditionID, questionID string) []byte {
	return []byte(`{
		"slug":"` + slug + `","title":"T","description":"D",
		"category_id":"` + categoryID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"market":{
			"slug":"` + slug + `-m1","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(conditionID) + `,
			"condition_id":"` + conditionID + `","question_id":"` + questionID + `",
			"tick_size":"0.01","min_size":5
		}
	}`)
}

func negRiskEventSingleMarketBody(slug, categoryID, negRiskMarketID, questionID string) []byte {
	return []byte(`{
		"slug":"` + slug + `","title":"T","description":"D",
		"category_id":"` + categoryID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negRiskMarketID + `",
		"market":{
			"slug":"` + slug + `-m1","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(questionID) + `,
			"question_id":"` + questionID + `",
			"tick_size":"0.01","min_size":5
		}
	}`)
}

func addBinaryMarketBody(slug, conditionID, questionID string) []byte {
	return []byte(`{
		"market":{
			"slug":"` + slug + `","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(conditionID) + `,
			"condition_id":"` + conditionID + `","question_id":"` + questionID + `",
			"tick_size":"0.01","min_size":5
		}
	}`)
}

func addNegRiskMarketBody(slug, questionID string) []byte {
	return []byte(`{
		"market":{
			"slug":"` + slug + `","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(questionID) + `,
			"question_id":"` + questionID + `",
			"tick_size":"0.01","min_size":5
		}
	}`)
}

func TestHandler_CreateEvent_Binary_AcceptsSingularMarketPayload(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	categoryID := seedCatID(t, repo, "issue94-binary-create")
	publisher := &fakePublisher{}
	chain := newFakeChainReader()
	conditionID := fixedCondHash("issue94-binary-create-condition")
	chain.slotCount[common.HexToHash(conditionID)] = 2
	mux := muxWithChain(t, repo, publisher, chain)

	recorder := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventSingleMarketBody("issue94-binary-create", categoryID, conditionID, fixedCondHash("issue94-binary-create-question")))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", recorder.Code, recorder.Body.String())
	}
	var response eventWithMarketsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Markets) != 1 {
		t.Fatalf("created markets = %d, want exactly 1", len(response.Markets))
	}
	if len(publisher.configCalls) != 1 {
		t.Fatalf("PublishMarketConfig calls = %d, want 1", len(publisher.configCalls))
	}
}

func TestHandler_CreateEvent_NegRisk_AcceptsSingularInitialMarket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	categoryID := seedCatID(t, repo, "issue94-neg-create")
	publisher := &fakePublisher{}
	chain := newFakeChainReader()
	negRiskMarketID := negRiskMarketIDHex("issue94-neg-create")
	questionID := negRiskQuestionID(negRiskMarketID, 1)
	conditionID := common.HexToHash(fixedCondHash("issue94-neg-condition"))
	chain.negRiskCondIDs[common.HexToHash(questionID)] = conditionID
	chain.slotCount[conditionID] = 2
	mux := muxWithChain(t, repo, publisher, chain)

	recorder := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk",
		negRiskEventSingleMarketBody("issue94-neg-create", categoryID, negRiskMarketID, questionID))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", recorder.Code, recorder.Body.String())
	}
	var response eventWithMarketsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Event.NegRiskMarketID == nil || *response.Event.NegRiskMarketID != negRiskMarketID {
		t.Fatalf("event.neg_risk_market_id = %v, want %s", response.Event.NegRiskMarketID, negRiskMarketID)
	}
	if len(response.Markets) != 1 {
		t.Fatalf("created markets = %d, want exactly 1", len(response.Markets))
	}
	if response.Markets[0].ConditionID != conditionID.Hex() {
		t.Fatalf("stored condition_id = %s, want %s", response.Markets[0].ConditionID, conditionID.Hex())
	}
}

func TestHandler_AddMarket_Binary_AppendsOneMarket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	publisher := &fakePublisher{}
	chain := newFakeChainReader()
	categoryID := seedCatID(t, repo, "issue94-binary-append")
	initialConditionID := fixedCondHash("issue94-binary-initial-condition")
	appendConditionID := fixedCondHash("issue94-binary-append-condition")
	chain.slotCount[common.HexToHash(initialConditionID)] = 2
	chain.slotCount[common.HexToHash(appendConditionID)] = 2
	mux := muxWithChain(t, repo, publisher, chain)

	created := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventSingleMarketBody("issue94-binary-append", categoryID, initialConditionID, fixedCondHash("issue94-binary-initial-question")))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%q, want 201", created.Code, created.Body.String())
	}
	var createdResponse eventWithMarketsResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	appended := doRequest(t, mux, http.MethodPost, "/admin/events/"+createdResponse.Event.ID+"/binary/markets",
		addBinaryMarketBody("issue94-binary-append-m2", appendConditionID, fixedCondHash("issue94-binary-append-question")))
	if appended.Code != http.StatusOK {
		t.Fatalf("append status = %d body=%q, want 200", appended.Code, appended.Body.String())
	}
	var appendedResponse eventWithMarketsResponse
	if err := json.Unmarshal(appended.Body.Bytes(), &appendedResponse); err != nil {
		t.Fatalf("decode append: %v", err)
	}
	if len(appendedResponse.Markets) != 2 {
		t.Fatalf("event markets after append = %d, want 2", len(appendedResponse.Markets))
	}
	if len(publisher.configCalls) != 2 {
		t.Fatalf("PublishMarketConfig calls = %d, want create + append", len(publisher.configCalls))
	}
}

func TestHandler_AddMarket_NegRisk_UsesStoredMarketID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	publisher := &fakePublisher{}
	chain := newFakeChainReader()
	categoryID := seedCatID(t, repo, "issue94-neg-append")
	negRiskMarketID := negRiskMarketIDHex("issue94-neg-append")
	initialQuestionID := negRiskQuestionID(negRiskMarketID, 1)
	appendQuestionID := negRiskQuestionID(negRiskMarketID, 2)
	initialConditionID := common.HexToHash(fixedCondHash("issue94-neg-initial-condition"))
	appendConditionID := common.HexToHash(fixedCondHash("issue94-neg-append-condition"))
	chain.negRiskCondIDs[common.HexToHash(initialQuestionID)] = initialConditionID
	chain.negRiskCondIDs[common.HexToHash(appendQuestionID)] = appendConditionID
	chain.slotCount[initialConditionID] = 2
	chain.slotCount[appendConditionID] = 2
	mux := muxWithChain(t, repo, publisher, chain)

	created := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk",
		negRiskEventSingleMarketBody("issue94-neg-append", categoryID, negRiskMarketID, initialQuestionID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%q, want 201", created.Code, created.Body.String())
	}
	var createdResponse eventWithMarketsResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	appended := doRequest(t, mux, http.MethodPost, "/admin/events/"+createdResponse.Event.ID+"/neg-risk/markets",
		addNegRiskMarketBody("issue94-neg-append-m2", appendQuestionID))
	if appended.Code != http.StatusOK {
		t.Fatalf("append status = %d body=%q, want 200", appended.Code, appended.Body.String())
	}
	if strings.Contains(appended.Body.String(), "neg_risk_market_id is required") {
		t.Fatalf("append required redundant neg_risk_market_id: %q", appended.Body.String())
	}
	var appendedResponse eventWithMarketsResponse
	if err := json.Unmarshal(appended.Body.Bytes(), &appendedResponse); err != nil {
		t.Fatalf("decode append: %v", err)
	}
	if len(appendedResponse.Markets) != 2 {
		t.Fatalf("event markets after append = %d, want 2", len(appendedResponse.Markets))
	}
}

func TestHandler_AddMarket_NegRisk_RejectsQuestionFromDifferentMarketID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	chain := newFakeChainReader()
	categoryID := seedCatID(t, repo, "issue94-neg-wrong-parent")
	negRiskMarketID := negRiskMarketIDHex("issue94-neg-wrong-parent")
	initialQuestionID := negRiskQuestionID(negRiskMarketID, 1)
	initialConditionID := common.HexToHash(fixedCondHash("issue94-neg-wrong-parent-condition"))
	chain.negRiskCondIDs[common.HexToHash(initialQuestionID)] = initialConditionID
	chain.slotCount[initialConditionID] = 2
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	created := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk",
		negRiskEventSingleMarketBody("issue94-neg-wrong-parent", categoryID, negRiskMarketID, initialQuestionID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%q, want 201", created.Code, created.Body.String())
	}
	var createdResponse eventWithMarketsResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	wrongParentQuestionID := negRiskQuestionID(negRiskMarketIDHex("issue94-other-parent"), 1)

	appended := doRequest(t, mux, http.MethodPost, "/admin/events/"+createdResponse.Event.ID+"/neg-risk/markets",
		addNegRiskMarketBody("issue94-neg-wrong-parent-m2", wrongParentQuestionID))
	if appended.Code != http.StatusBadRequest {
		t.Fatalf("append status = %d body=%q, want 400", appended.Code, appended.Body.String())
	}
	testassert.BodyContains(t, appended, "question_id does not belong to neg_risk_market_id")
}
