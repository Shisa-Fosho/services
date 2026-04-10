package affiliate

import (
	"errors"
	"testing"
)

func TestValidateReferral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(r *Referral)
		wantErr bool
		errType error
	}{
		{
			name:    "valid referral",
			modify:  func(r *Referral) {},
			wantErr: false,
		},
		{
			name:    "empty referrer address",
			modify:  func(r *Referral) { r.ReferrerAddress = "" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "invalid referrer address format",
			modify:  func(r *Referral) { r.ReferrerAddress = "not-an-address" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "referrer address too short",
			modify:  func(r *Referral) { r.ReferrerAddress = "0x1234" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "empty referred address",
			modify:  func(r *Referral) { r.ReferredAddress = "" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "invalid referred address format",
			modify:  func(r *Referral) { r.ReferredAddress = "0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name: "self referral",
			modify: func(r *Referral) {
				r.ReferredAddress = r.ReferrerAddress
			},
			wantErr: true,
			errType: ErrSelfReferral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := &Referral{
				ReferrerAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ReferredAddress: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			}
			tt.modify(ref)
			err := ValidateReferral(ref)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("expected %v, got: %v", tt.errType, err)
			}
		})
	}
}

func validEarning() *Earning {
	return &Earning{
		ReferrerAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TradeID:         "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		FeeAmount:       100,
		ReferrerCut:     25,
	}
}

func TestValidateEarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(e *Earning)
		wantErr bool
	}{
		{
			name:    "valid earning passes",
			modify:  func(e *Earning) {},
			wantErr: false,
		},
		{
			name:    "empty referrer address",
			modify:  func(e *Earning) { e.ReferrerAddress = "" },
			wantErr: true,
		},
		{
			name:    "malformed referrer address",
			modify:  func(e *Earning) { e.ReferrerAddress = "not-an-address" },
			wantErr: true,
		},
		{
			name:    "empty trade id",
			modify:  func(e *Earning) { e.TradeID = "" },
			wantErr: true,
		},
		{
			name:    "zero fee amount",
			modify:  func(e *Earning) { e.FeeAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative fee amount",
			modify:  func(e *Earning) { e.FeeAmount = -1 },
			wantErr: true,
		},
		{
			name:    "zero referrer cut",
			modify:  func(e *Earning) { e.ReferrerCut = 0 },
			wantErr: true,
		},
		{
			name:    "negative referrer cut",
			modify:  func(e *Earning) { e.ReferrerCut = -1 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := validEarning()
			tt.modify(e)
			err := ValidateEarning(e)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidEarning) {
				t.Errorf("expected ErrInvalidEarning, got: %v", err)
			}
		})
	}
}
