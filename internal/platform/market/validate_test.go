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
			name: "with category ID is valid",
			modify: func(event *Event) {
				catID := "550e8400-e29b-41d4-a716-446655440000"
				event.CategoryID = &catID
			},
			wantErr: false,
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
