package data

import "testing"

func TestSignupMethod_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method SignupMethod
		want   string
	}{
		{"wallet", SignupMethodWallet, "WALLET"},
		{"email", SignupMethodEmail, "EMAIL"},
		{"unknown", SignupMethod(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.method.String(); got != tt.want {
				t.Errorf("SignupMethod(%d).String() = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestSignupMethod_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method SignupMethod
		want   bool
	}{
		{"wallet", SignupMethodWallet, true},
		{"email", SignupMethodEmail, true},
		{"negative", SignupMethod(-1), false},
		{"out_of_range", SignupMethod(5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.method.IsValid(); got != tt.want {
				t.Errorf("SignupMethod(%d).IsValid() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestSide_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		side Side
		want string
	}{
		{"buy", SideBuy, "BUY"},
		{"sell", SideSell, "SELL"},
		{"unknown", Side(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.side.String(); got != tt.want {
				t.Errorf("Side(%d).String() = %q, want %q", tt.side, got, tt.want)
			}
		})
	}
}

func TestSide_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		side Side
		want bool
	}{
		{"buy", SideBuy, true},
		{"sell", SideSell, true},
		{"negative", Side(-1), false},
		{"out_of_range", Side(5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.side.IsValid(); got != tt.want {
				t.Errorf("Side(%d).IsValid() = %v, want %v", tt.side, got, tt.want)
			}
		})
	}
}
