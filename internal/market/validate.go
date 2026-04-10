package market

import (
	"fmt"
	"time"
)

// ValidateEvent checks that an event meets all creation rules.
// now is the current time, passed explicitly for testability.
// Returns ErrInvalidEvent wrapping a descriptive message on failure.
func ValidateEvent(event *Event, now time.Time) error {
	if event.Slug == "" {
		return fmt.Errorf("slug is required: %w", ErrInvalidEvent)
	}

	if event.Title == "" {
		return fmt.Errorf("title is required: %w", ErrInvalidEvent)
	}

	if !event.EndDate.After(now) {
		return fmt.Errorf("end date must be in the future: %w", ErrInvalidEvent)
	}

	if !event.EventType.IsValid() {
		return fmt.Errorf("invalid event type %d: %w", event.EventType, ErrInvalidEvent)
	}

	if !event.Status.IsValid() {
		return fmt.Errorf("invalid status %d: %w", event.Status, ErrInvalidEvent)
	}

	return nil
}

// ValidateMarket checks that a market meets all creation rules.
// Returns ErrInvalidMarket wrapping a descriptive message on failure.
func ValidateMarket(market *Market) error {
	if market.Slug == "" {
		return fmt.Errorf("slug is required: %w", ErrInvalidMarket)
	}

	if market.Question == "" {
		return fmt.Errorf("question is required: %w", ErrInvalidMarket)
	}

	if market.OutcomeYesLabel == "" {
		return fmt.Errorf("outcome yes label is required: %w", ErrInvalidMarket)
	}

	if market.OutcomeNoLabel == "" {
		return fmt.Errorf("outcome no label is required: %w", ErrInvalidMarket)
	}

	if !market.Status.IsValid() {
		return fmt.Errorf("invalid status %d: %w", market.Status, ErrInvalidMarket)
	}

	if market.TokenIDYes == "" {
		return fmt.Errorf("token ID yes is required: %w", ErrInvalidMarket)
	}

	if market.TokenIDNo == "" {
		return fmt.Errorf("token ID no is required: %w", ErrInvalidMarket)
	}

	if market.ConditionID == "" {
		return fmt.Errorf("condition ID is required: %w", ErrInvalidMarket)
	}

	return nil
}

// ValidateStatusTransition checks that a status change is allowed.
// Returns ErrInvalidTransition if the transition is not permitted.
func ValidateStatusTransition(from, to Status) error {
	if !ValidTransition(from, to) {
		return fmt.Errorf("cannot transition from %s to %s: %w", from, to, ErrInvalidTransition)
	}
	return nil
}
