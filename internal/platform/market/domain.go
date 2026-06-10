// Package market defines the core domain types for the prediction market
// platform service: events, markets, categories, and their associated enumerations.
package market

import (
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors for the market domain.
var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidEvent      = errors.New("invalid event")
	ErrInvalidMarket     = errors.New("invalid market")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrDuplicateSlug     = errors.New("duplicate slug")
	ErrInvalidFeeRate    = errors.New("invalid fee rate")
	// ErrOnChainMismatch is returned when on-chain state contradicts an
	// admin-declared outcome (e.g. admin says YES but payoutNumerators
	// show [0,1]). Handlers map it to 422.
	ErrOnChainMismatch = errors.New("on-chain state mismatch")
)

// Fee-rate bounds (basis points). MaxFeeBps matches the hard-coded on-chain
// ceiling in Polymarket/ctf-exchange::Fees.sol (MAX_FEE_RATE_BIPS = 1000).
// Setting a rate above this would cause the exchange to revert on fill.
const (
	MinFeeBps = 0
	MaxFeeBps = 1000 // 10%
)

// EventType represents the on-chain settlement mechanics for an event.
// BINARY markets settle directly against ConditionalTokens. NEG_RISK markets
// settle via the NegRiskAdapter with mutual-exclusivity semantics — at most
// one constituent binary question may resolve YES.
type EventType int8

// EventType values.
const (
	EventTypeBinary  EventType = 0
	EventTypeNegRisk EventType = 1
)

func (eventType EventType) String() string {
	switch eventType {
	case EventTypeBinary:
		return "BINARY"
	case EventTypeNegRisk:
		return "NEG_RISK"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the event type is a known value.
func (eventType EventType) IsValid() bool {
	return eventType == EventTypeBinary || eventType == EventTypeNegRisk
}

// ParseEventType decodes the SDK string ("BINARY" | "NEG_RISK") to the
// corresponding enum value. Returns false if the string is unrecognized.
func ParseEventType(value string) (EventType, bool) {
	switch value {
	case "BINARY":
		return EventTypeBinary, true
	case "NEG_RISK":
		return EventTypeNegRisk, true
	default:
		return 0, false
	}
}

// Status represents the lifecycle state of an event or market.
type Status int8

// Status values.
const (
	StatusActive   Status = 0
	StatusPaused   Status = 1
	StatusResolved Status = 2
	StatusVoided   Status = 3
)

func (status Status) String() string {
	switch status {
	case StatusActive:
		return "ACTIVE"
	case StatusPaused:
		return "PAUSED"
	case StatusResolved:
		return "RESOLVED"
	case StatusVoided:
		return "VOIDED"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the status is a known value.
func (status Status) IsValid() bool {
	return status >= StatusActive && status <= StatusVoided
}

// IsTerminal returns true if the status is one of the terminal states
// (Resolved or Voided) — meaning no further transitions are allowed.
func (status Status) IsTerminal() bool {
	return status == StatusResolved || status == StatusVoided
}

// Outcome represents the resolved outcome of a market.
type Outcome int8

// Outcome values.
const (
	OutcomeYes Outcome = 0
	OutcomeNo  Outcome = 1
)

func (outcome Outcome) String() string {
	switch outcome {
	case OutcomeYes:
		return "YES"
	case OutcomeNo:
		return "NO"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the outcome is a known value.
func (outcome Outcome) IsValid() bool {
	return outcome == OutcomeYes || outcome == OutcomeNo
}

// ParseOutcome decodes the SDK string ("YES" | "NO") to the corresponding
// enum value. Returns false if the string is unrecognized.
func ParseOutcome(value string) (Outcome, bool) {
	switch value {
	case "YES":
		return OutcomeYes, true
	case "NO":
		return OutcomeNo, true
	default:
		return 0, false
	}
}

// TickSize represents the minimum price increment for a market, encoded as
// an enum index matching the Polymarket clob-client v5.8.2 SDK strings.
type TickSize int8

// TickSize values. The index maps to a Polymarket SDK string:
//
//	0 -> "0.1"
//	1 -> "0.01"   (default — most common market shape)
//	2 -> "0.001"
//	3 -> "0.0001"
const (
	TickSize0_1    TickSize = 0
	TickSize0_01   TickSize = 1
	TickSize0_001  TickSize = 2
	TickSize0_0001 TickSize = 3
)

func (tickSize TickSize) String() string {
	switch tickSize {
	case TickSize0_1:
		return "0.1"
	case TickSize0_01:
		return "0.01"
	case TickSize0_001:
		return "0.001"
	case TickSize0_0001:
		return "0.0001"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the tick size is a known value.
func (tickSize TickSize) IsValid() bool {
	return tickSize >= TickSize0_1 && tickSize <= TickSize0_0001
}

// ParseTickSize decodes the SDK string to the corresponding enum value.
// Returns false if the string is unrecognized.
func ParseTickSize(value string) (TickSize, bool) {
	switch value {
	case "0.1":
		return TickSize0_1, true
	case "0.01":
		return TickSize0_01, true
	case "0.001":
		return TickSize0_001, true
	case "0.0001":
		return TickSize0_0001, true
	default:
		return 0, false
	}
}

// ValidTransition returns true if moving from one Status to another
// is allowed.
//
// Allowed transitions:
//
//	active  → paused, resolved, voided
//	paused  → active
//	resolved → (none — terminal)
//	voided   → (none — terminal)
func ValidTransition(from, to Status) bool {
	switch from {
	case to:
		return false
	case StatusActive:
		if to == StatusPaused || to == StatusResolved || to == StatusVoided {
			return true
		}
	case StatusPaused:
		if to == StatusActive {
			return true
		}
	case StatusResolved, StatusVoided:
		return false
	}
	return false
}

// Category represents a grouping for events (e.g., "Sports", "Politics").
type Category struct {
	ID   string `db:"id"` // UUID, generated by database.
	Name string `db:"name"`
	Slug string `db:"slug"` // URL-friendly unique identifier.
}

// Event represents a top-level prediction event that may contain one or more
// markets. For example, "2024 US Presidential Election" with markets for each
// candidate.
//
// NegRiskMarketID is the adapter-level marketId emitted by
// NegRiskAdapter.prepareMarket. It groups multiple binary questions under
// one mutually-exclusive umbrella. NOT to be confused with our DB UUID
// (Event.ID) — see docs/plans/p2.4-event-market-lifecycle.md.
type Event struct {
	ID                string          `db:"id"`   // UUID, generated by database.
	Slug              string          `db:"slug"` // URL-friendly unique identifier.
	Title             string          `db:"title"`
	Description       string          `db:"description"`
	CategoryID        string          `db:"category_id"` // FK to categories; required.
	EventType         EventType       `db:"event_type"`
	ResolutionConfig  json.RawMessage `db:"resolution_config"` // JSONB — always {} for manual resolution.
	Status            Status          `db:"status"`
	EndDate           time.Time       `db:"end_date"`
	Featured          bool            `db:"featured"`
	FeaturedSortOrder int16           `db:"featured_sort_order"`
	NegRiskMarketID   *string         `db:"neg_risk_market_id"` // adapter marketId; set only when event_type = NEG_RISK.
	CreatedAt         time.Time       `db:"created_at"`
	UpdatedAt         time.Time       `db:"updated_at"`
}

// EventUpdate describes a partial update to an event's editable metadata.
// Non-nil fields are applied; nil fields are left unchanged. Category can
// be changed to a different category but never cleared — the column is
// non-nullable and every event must belong to a category. Slug, event_type,
// end_date, resolution_config, and status are intentionally not included —
// those are set at creation or via the dedicated status-transition endpoint.
type EventUpdate struct {
	Title             *string
	Description       *string
	CategoryID        *string
	Featured          *bool
	FeaturedSortOrder *int16
}

// MarketUpdate describes a partial update to a market's editable metadata.
// Non-nil fields are applied; nil fields are left unchanged. Slug, token IDs,
// condition ID, prices, volume, open interest, and status are intentionally
// not editable via this path.
//
//nolint:revive // name mirrors EventUpdate and the external API shape.
type MarketUpdate struct {
	Question        *string
	OutcomeYesLabel *string
	OutcomeNoLabel  *string
}

// Market represents a single binary YES/NO prediction market. Every market
// belongs to an event (FK to events.id). All monetary amounts are in
// integer cents (1 = $0.01).
type Market struct {
	ID              string    `db:"id"`                // UUID, generated by database.
	Slug            string    `db:"slug"`              // URL-friendly unique identifier.
	EventID         string    `db:"event_id"`          // FK to events; every market belongs to an event.
	Question        string    `db:"question"`          // The prediction question.
	OutcomeYesLabel string    `db:"outcome_yes_label"` // Display label for YES outcome (default "Yes").
	OutcomeNoLabel  string    `db:"outcome_no_label"`  // Display label for NO outcome (default "No").
	TokenIDYes      string    `db:"token_id_yes"`      // Conditional token ID for YES outcome.
	TokenIDNo       string    `db:"token_id_no"`       // Conditional token ID for NO outcome.
	ConditionID     string    `db:"condition_id"`      // CT condition ID. For NEG_RISK derived server-side from QuestionID.
	QuestionID      string    `db:"question_id"`       // On-chain questionId; required for the admin to call reportPayouts.
	Status          Status    `db:"status"`
	Outcome         *Outcome  `db:"outcome"`       // Null until resolved.
	PriceYes        int64     `db:"price_yes"`     // Current YES price in cents (1-99).
	PriceNo         int64     `db:"price_no"`      // Current NO price in cents (1-99).
	Volume          int64     `db:"volume"`        // Total traded volume in cents.
	OpenInterest    int64     `db:"open_interest"` // Current open interest in cents.
	FeeRateBps      *int64    `db:"fee_rate_bps"`  // Nullable; nil means "use platform default" (0 bps).
	TickSize        TickSize  `db:"tick_size"`     // Polymarket SDK tick-size enum.
	MinSize         int64     `db:"min_size"`      // Minimum order size in cents.
	MaxSize         *int64    `db:"max_size"`      // Optional max order size; nil = unlimited.
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}
