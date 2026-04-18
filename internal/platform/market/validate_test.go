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
		modify  func(e *Event)
		wantErr bool
	}{
		{
			name:    "valid event passes",
			modify:  func(e *Event) {},
			wantErr: false,
		},
		{
			name:    "empty slug",
			modify:  func(e *Event) { e.Slug = "" },
			wantErr: true,
		},
		{
			name:    "empty title",
			modify:  func(e *Event) { e.Title = "" },
			wantErr: true,
		},
		{
			name:    "end date in the past",
			modify:  func(e *Event) { e.EndDate = time.Now().Add(-1 * time.Hour) },
			wantErr: true,
		},
		{
			name:    "invalid event type",
			modify:  func(e *Event) { e.EventType = EventType(99) },
			wantErr: true,
		},
		{
			name:    "invalid status",
			modify:  func(e *Event) { e.Status = Status(99) },
			wantErr: true,
		},
		{
			name: "with category ID is valid",
			modify: func(e *Event) {
				catID := "550e8400-e29b-41d4-a716-446655440000"
				e.CategoryID = &catID
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := validEvent()
			tt.modify(e)
			err := ValidateEvent(e, time.Now())
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
		modify  func(m *Market)
		wantErr bool
	}{
		{
			name:    "valid market passes",
			modify:  func(m *Market) {},
			wantErr: false,
		},
		{
			name:    "empty slug",
			modify:  func(m *Market) { m.Slug = "" },
			wantErr: true,
		},
		{
			name:    "empty question",
			modify:  func(m *Market) { m.Question = "" },
			wantErr: true,
		},
		{
			name:    "empty yes label",
			modify:  func(m *Market) { m.OutcomeYesLabel = "" },
			wantErr: true,
		},
		{
			name:    "empty no label",
			modify:  func(m *Market) { m.OutcomeNoLabel = "" },
			wantErr: true,
		},
		{
			name:    "invalid status",
			modify:  func(m *Market) { m.Status = Status(99) },
			wantErr: true,
		},
		{
			name:    "empty token ID yes",
			modify:  func(m *Market) { m.TokenIDYes = "" },
			wantErr: true,
		},
		{
			name:    "empty token ID no",
			modify:  func(m *Market) { m.TokenIDNo = "" },
			wantErr: true,
		},
		{
			name:    "empty condition ID",
			modify:  func(m *Market) { m.ConditionID = "" },
			wantErr: true,
		},
		{
			name: "with event ID is valid",
			modify: func(m *Market) {
				eventID := "550e8400-e29b-41d4-a716-446655440000"
				m.EventID = &eventID
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := validMarket()
			tt.modify(m)
			err := ValidateMarket(m)
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
