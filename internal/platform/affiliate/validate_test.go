package affiliate

import (
	"errors"
	"testing"
)

func TestValidateReferral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(ref *Referral)
		wantErr bool
		errType error
	}{
		{
			name:    "valid referral",
			modify:  func(ref *Referral) {},
			wantErr: false,
		},
		{
			name:    "empty referrer address",
			modify:  func(ref *Referral) { ref.ReferrerAddress = "" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "invalid referrer address format",
			modify:  func(ref *Referral) { ref.ReferrerAddress = "not-an-address" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "referrer address too short",
			modify:  func(ref *Referral) { ref.ReferrerAddress = "0x1234" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "empty referred address",
			modify:  func(ref *Referral) { ref.ReferredAddress = "" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name:    "invalid referred address format",
			modify:  func(ref *Referral) { ref.ReferredAddress = "0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ" },
			wantErr: true,
			errType: ErrInvalidReferral,
		},
		{
			name: "self referral",
			modify: func(ref *Referral) {
				ref.ReferredAddress = ref.ReferrerAddress
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
		modify  func(earning *Earning)
		wantErr bool
	}{
		{
			name:    "valid earning passes",
			modify:  func(earning *Earning) {},
			wantErr: false,
		},
		{
			name:    "empty referrer address",
			modify:  func(earning *Earning) { earning.ReferrerAddress = "" },
			wantErr: true,
		},
		{
			name:    "malformed referrer address",
			modify:  func(earning *Earning) { earning.ReferrerAddress = "not-an-address" },
			wantErr: true,
		},
		{
			name:    "empty trade id",
			modify:  func(earning *Earning) { earning.TradeID = "" },
			wantErr: true,
		},
		{
			name:    "zero fee amount",
			modify:  func(earning *Earning) { earning.FeeAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative fee amount",
			modify:  func(earning *Earning) { earning.FeeAmount = -1 },
			wantErr: true,
		},
		{
			name:    "zero referrer cut",
			modify:  func(earning *Earning) { earning.ReferrerCut = 0 },
			wantErr: true,
		},
		{
			name:    "negative referrer cut",
			modify:  func(earning *Earning) { earning.ReferrerCut = -1 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			earning := validEarning()
			tt.modify(earning)
			err := ValidateEarning(earning)
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
