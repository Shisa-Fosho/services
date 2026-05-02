package market

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func validEvent() *Event {
	return &Event{
		Slug:             "test-event",
		Title:            "Test Event",
		Description:      "A test event.",
		CategoryID:       "550e8400-e29b-41d4-a716-446655440000",
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
}

func TestValidateEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(event *Event)
		wantErr bool
	}{
		{
			name:    "valid event passes",
			modify:  func(event *Event) {},
			wantErr: false,
		},
		{
			name:    "empty slug",
			modify:  func(event *Event) { event.Slug = "" },
			wantErr: true,
		},
		{
			name:    "empty title",
			modify:  func(event *Event) { event.Title = "" },
			wantErr: true,
		},
		{
			name:    "end date in the past",
			modify:  func(event *Event) { event.EndDate = time.Now().Add(-1 * time.Hour) },
			wantErr: true,
		},
		{
			name:    "invalid event type",
			modify:  func(event *Event) { event.EventType = EventType(99) },
			wantErr: true,
		},
		{
			name:    "invalid status",
			modify:  func(event *Event) { event.Status = Status(99) },
			wantErr: true,
		},
		{
			name:    "empty category id",
			modify:  func(event *Event) { event.CategoryID = "" },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := validEvent()
			tt.modify(event)
			err := ValidateEvent(event, time.Now())
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidEvent) {
				t.Errorf("expected ErrInvalidEvent, got: %v", err)
			}
		})
	}
}

func validMarket() *Market {
	return &Market{
		Slug:            "test-market",
		Question:        "Will it happen?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "token-yes-123",
		TokenIDNo:       "token-no-456",
		ConditionID:     "condition-789",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
}

func TestValidateMarket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(market *Market)
		wantErr bool
	}{
		{
			name:    "valid market passes",
			modify:  func(market *Market) {},
			wantErr: false,
		},
		{
			name:    "empty slug",
			modify:  func(market *Market) { market.Slug = "" },
			wantErr: true,
		},
		{
			name:    "empty question",
			modify:  func(market *Market) { market.Question = "" },
			wantErr: true,
		},
		{
			name:    "empty yes label",
			modify:  func(market *Market) { market.OutcomeYesLabel = "" },
			wantErr: true,
		},
		{
			name:    "empty no label",
			modify:  func(market *Market) { market.OutcomeNoLabel = "" },
			wantErr: true,
		},
		{
			name:    "invalid status",
			modify:  func(market *Market) { market.Status = Status(99) },
			wantErr: true,
		},
		{
			name:    "empty token ID yes",
			modify:  func(market *Market) { market.TokenIDYes = "" },
			wantErr: true,
		},
		{
			name:    "empty token ID no",
			modify:  func(market *Market) { market.TokenIDNo = "" },
			wantErr: true,
		},
		{
			name:    "empty condition ID",
			modify:  func(market *Market) { market.ConditionID = "" },
			wantErr: true,
		},
		{
			name: "with event ID is valid",
			modify: func(market *Market) {
				eventID := "550e8400-e29b-41d4-a716-446655440000"
				market.EventID = &eventID
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			market := validMarket()
			tt.modify(market)
			err := ValidateMarket(market)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidMarket) {
				t.Errorf("expected ErrInvalidMarket, got: %v", err)
			}
		})
	}
}

func TestValidateEventUpdate(t *testing.T) {
	t.Parallel()

	title := "New"
	empty := ""
	catID := "550e8400-e29b-41d4-a716-446655440000"
	featured := true
	validOrder := int16(10)
	negOrder := int16(-1)

	tests := []struct {
		name    string
		update  *EventUpdate
		wantErr bool
	}{
		{"nil update", nil, true},
		{"all fields nil is empty", &EventUpdate{}, true},
		{"title only is valid", &EventUpdate{Title: &title}, false},
		{"featured only is valid", &EventUpdate{Featured: &featured}, false},
		{"set category with uuid is valid", &EventUpdate{CategoryID: &catID}, false},
		{"empty title is invalid", &EventUpdate{Title: &empty}, true},
		{"empty category id is invalid", &EventUpdate{CategoryID: &empty}, true},
		{"negative sort order is invalid", &EventUpdate{FeaturedSortOrder: &negOrder}, true},
		{"valid sort order", &EventUpdate{FeaturedSortOrder: &validOrder}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEventUpdate(tt.update)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidEvent) {
				t.Errorf("expected ErrInvalidEvent, got: %v", err)
			}
		})
	}
}

func TestValidateMarketUpdate(t *testing.T) {
	t.Parallel()

	question := "Will it?"
	yes := "YES"
	no := "NO"
	empty := ""

	tests := []struct {
		name    string
		update  *MarketUpdate
		wantErr bool
	}{
		{"nil update", nil, true},
		{"empty update", &MarketUpdate{}, true},
		{"question only is valid", &MarketUpdate{Question: &question}, false},
		{"yes label only is valid", &MarketUpdate{OutcomeYesLabel: &yes}, false},
		{"no label only is valid", &MarketUpdate{OutcomeNoLabel: &no}, false},
		{"empty question is invalid", &MarketUpdate{Question: &empty}, true},
		{"empty yes label is invalid", &MarketUpdate{OutcomeYesLabel: &empty}, true},
		{"empty no label is invalid", &MarketUpdate{OutcomeNoLabel: &empty}, true},
		{"all fields set is valid", &MarketUpdate{Question: &question, OutcomeYesLabel: &yes, OutcomeNoLabel: &no}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMarketUpdate(tt.update)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidMarket) {
				t.Errorf("expected ErrInvalidMarket, got: %v", err)
			}
		})
	}
}

func TestValidateFeeRateBps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bps     int
		wantErr bool
	}{
		{"zero is valid", 0, false},
		{"max is valid", MaxFeeBps, false},
		{"mid range valid", 25, false},
		{"negative bps", -1, true},
		{"above on-chain cap", MaxFeeBps + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateFeeRateBps(tt.bps)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidFeeRate) {
				t.Errorf("expected ErrInvalidFeeRate, got: %v", err)
			}
		})
	}
}

func TestValidateStatusTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr bool
	}{
		{"valid active to paused", StatusActive, StatusPaused, false},
		{"valid paused to active", StatusPaused, StatusActive, false},
		{"invalid resolved to active", StatusResolved, StatusActive, true},
		{"invalid voided to active", StatusVoided, StatusActive, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateStatusTransition(tt.from, tt.to)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("expected ErrInvalidTransition, got: %v", err)
			}
		})
	}
}
