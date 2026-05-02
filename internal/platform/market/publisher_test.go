package market

import (
	"encoding/json"
	"testing"
)

func TestToConfigEntry_AllFieldsCopied(t *testing.T) {
	t.Parallel()
	feeRate := int64(50)
	market := &Market{
		ID:         "mkt-1",
		Status:     StatusPaused,
		TokenIDYes: "token-yes",
		TokenIDNo:  "token-no",
		FeeRateBps: &feeRate,
	}

	entry := toConfigEntry(market)

	if entry.MarketID != market.ID {
		t.Errorf("MarketID = %q, want %q", entry.MarketID, market.ID)
	}
	if entry.Status != "PAUSED" {
		t.Errorf("Status = %q, want PAUSED", entry.Status)
	}
	if entry.TokenIDYes != market.TokenIDYes {
		t.Errorf("TokenIDYes = %q, want %q", entry.TokenIDYes, market.TokenIDYes)
	}
	if entry.TokenIDNo != market.TokenIDNo {
		t.Errorf("TokenIDNo = %q, want %q", entry.TokenIDNo, market.TokenIDNo)
	}
	if entry.FeeRateBps == nil || *entry.FeeRateBps != feeRate {
		t.Errorf("FeeRateBps = %v, want pointer to %d", entry.FeeRateBps, feeRate)
	}
}

func TestToConfigEntry_NilFeeRatePassesThrough(t *testing.T) {
	t.Parallel()
	market := &Market{
		ID:         "mkt-1",
		Status:     StatusActive,
		TokenIDYes: "y",
		TokenIDNo:  "n",
		FeeRateBps: nil, // "use platform default"
	}

	entry := toConfigEntry(market)

	if entry.FeeRateBps != nil {
		t.Errorf("FeeRateBps = %v, want nil", *entry.FeeRateBps)
	}
}

func TestConfigEntry_JSONShape_FeeRateOmittedWhenNil(t *testing.T) {
	t.Parallel()
	market := &Market{
		ID:         "abc-123",
		Status:     StatusActive,
		TokenIDYes: "y",
		TokenIDNo:  "n",
		FeeRateBps: nil,
	}
	data, err := json.Marshal(toConfigEntry(market))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Decode into a generic map to verify exact wire shape.
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got["market_id"] != "abc-123" {
		t.Errorf("market_id = %v, want abc-123", got["market_id"])
	}
	if got["status"] != "ACTIVE" {
		t.Errorf("status = %v, want ACTIVE", got["status"])
	}
	if _, present := got["fee_rate_bps"]; present {
		t.Errorf("fee_rate_bps should be omitted when nil, got %v", got["fee_rate_bps"])
	}
}

func TestConfigEntry_JSONShape_FeeRateIncludedWhenSet(t *testing.T) {
	t.Parallel()
	feeRate := int64(75)
	market := &Market{
		ID:         "abc-123",
		Status:     StatusActive,
		TokenIDYes: "y",
		TokenIDNo:  "n",
		FeeRateBps: &feeRate,
	}
	data, err := json.Marshal(toConfigEntry(market))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	raw, ok := got["fee_rate_bps"]
	if !ok {
		t.Fatalf("fee_rate_bps missing from payload")
	}
	if num, _ := raw.(float64); num != 75 {
		t.Errorf("fee_rate_bps = %v, want 75", raw)
	}
}
