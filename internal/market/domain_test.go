package market

import "testing"

func TestEventType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		et   EventType
		want string
	}{
		{"binary", EventTypeBinary, "BINARY"},
		{"multi_outcome", EventTypeMultiOutcome, "MULTI_OUTCOME"},
		{"unknown", EventType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.et.String(); got != tt.want {
				t.Errorf("EventType(%d).String() = %q, want %q", tt.et, got, tt.want)
			}
		})
	}
}

func TestEventType_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		et   EventType
		want bool
	}{
		{"binary", EventTypeBinary, true},
		{"multi_outcome", EventTypeMultiOutcome, true},
		{"negative", EventType(-1), false},
		{"out_of_range", EventType(5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.et.IsValid(); got != tt.want {
				t.Errorf("EventType(%d).IsValid() = %v, want %v", tt.et, got, tt.want)
			}
		})
	}
}

func TestStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{"active", StatusActive, "ACTIVE"},
		{"paused", StatusPaused, "PAUSED"},
		{"resolved", StatusResolved, "RESOLVED"},
		{"voided", StatusVoided, "VOIDED"},
		{"unknown", Status(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatus_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"active", StatusActive, true},
		{"paused", StatusPaused, true},
		{"resolved", StatusResolved, true},
		{"voided", StatusVoided, true},
		{"negative", Status(-1), false},
		{"out_of_range", Status(10), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("Status(%d).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestOutcome_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outcome Outcome
		want    string
	}{
		{"yes", OutcomeYes, "YES"},
		{"no", OutcomeNo, "NO"},
		{"unknown", Outcome(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.outcome.String(); got != tt.want {
				t.Errorf("Outcome(%d).String() = %q, want %q", tt.outcome, got, tt.want)
			}
		})
	}
}

func TestOutcome_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outcome Outcome
		want    bool
	}{
		{"yes", OutcomeYes, true},
		{"no", OutcomeNo, true},
		{"negative", Outcome(-1), false},
		{"out_of_range", Outcome(5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.outcome.IsValid(); got != tt.want {
				t.Errorf("Outcome(%d).IsValid() = %v, want %v", tt.outcome, got, tt.want)
			}
		})
	}
}

func TestValidTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		// Valid transitions from active.
		{"active_to_paused", StatusActive, StatusPaused, true},
		{"active_to_resolved", StatusActive, StatusResolved, true},
		{"active_to_voided", StatusActive, StatusVoided, true},

		// Valid transition from paused.
		{"paused_to_active", StatusPaused, StatusActive, true},

		// Self-transitions (not allowed).
		{"active_to_active", StatusActive, StatusActive, false},
		{"paused_to_paused", StatusPaused, StatusPaused, false},
		{"resolved_to_resolved", StatusResolved, StatusResolved, false},
		{"voided_to_voided", StatusVoided, StatusVoided, false},

		// Invalid transitions from paused.
		{"paused_to_resolved", StatusPaused, StatusResolved, false},
		{"paused_to_voided", StatusPaused, StatusVoided, false},

		// Terminal states — no transitions out.
		{"resolved_to_active", StatusResolved, StatusActive, false},
		{"resolved_to_paused", StatusResolved, StatusPaused, false},
		{"resolved_to_voided", StatusResolved, StatusVoided, false},
		{"voided_to_active", StatusVoided, StatusActive, false},
		{"voided_to_paused", StatusVoided, StatusPaused, false},
		{"voided_to_resolved", StatusVoided, StatusResolved, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("ValidTransition(%s, %s) = %v, want %v",
					tt.from, tt.to, got, tt.want)
			}
		})
	}
}
