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

	if event.CategoryID == "" {
		return fmt.Errorf("category_id is required: %w", ErrInvalidEvent)
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

// ValidateEventUpdate checks a partial event update for shape violations.
// Enforces non-empty strings when a field is being set, and a non-negative
// featured_sort_order. Returns ErrInvalidEvent wrapping a descriptive message.
func ValidateEventUpdate(update *EventUpdate) error {
	if update == nil ||
		(update.Title == nil &&
			update.Description == nil &&
			update.CategoryID == nil &&
			update.Featured == nil &&
			update.FeaturedSortOrder == nil) {
		return fmt.Errorf("no fields to update: %w", ErrInvalidEvent)
	}
	if update.Title != nil && *update.Title == "" {
		return fmt.Errorf("title cannot be empty: %w", ErrInvalidEvent)
	}
	if update.CategoryID != nil && *update.CategoryID == "" {
		return fmt.Errorf("category_id cannot be empty: %w", ErrInvalidEvent)
	}
	if update.FeaturedSortOrder != nil && *update.FeaturedSortOrder < 0 {
		return fmt.Errorf("featured_sort_order must be non-negative: %w", ErrInvalidEvent)
	}
	return nil
}

// ValidateFeeRate checks that the market id is non-empty and the fee rate
// is within [MinFeeBps, MaxFeeBps]. Returns ErrInvalidFeeRate wrapping a
// descriptive message on failure.
func ValidateFeeRate(rate *FeeRate) error {
	if rate == nil {
		return fmt.Errorf("rate is nil: %w", ErrInvalidFeeRate)
	}
	if rate.MarketID == "" {
		return fmt.Errorf("market_id is required: %w", ErrInvalidFeeRate)
	}
	if rate.FeeRateBps < MinFeeBps || rate.FeeRateBps > MaxFeeBps {
		return fmt.Errorf("fee_rate_bps %d out of range [%d, %d]: %w",
			rate.FeeRateBps, MinFeeBps, MaxFeeBps, ErrInvalidFeeRate)
	}
	return nil
}

// ValidateMarketUpdate checks a partial market update for shape violations.
// Enforces non-empty strings when a field is being set. Returns
// ErrInvalidMarket wrapping a descriptive message.
func ValidateMarketUpdate(update *MarketUpdate) error {
	if update == nil ||
		(update.Question == nil &&
			update.OutcomeYesLabel == nil &&
			update.OutcomeNoLabel == nil) {
		return fmt.Errorf("no fields to update: %w", ErrInvalidMarket)
	}
	if update.Question != nil && *update.Question == "" {
		return fmt.Errorf("question cannot be empty: %w", ErrInvalidMarket)
	}
	if update.OutcomeYesLabel != nil && *update.OutcomeYesLabel == "" {
		return fmt.Errorf("outcome_yes_label cannot be empty: %w", ErrInvalidMarket)
	}
	if update.OutcomeNoLabel != nil && *update.OutcomeNoLabel == "" {
		return fmt.Errorf("outcome_no_label cannot be empty: %w", ErrInvalidMarket)
	}
	return nil
}
