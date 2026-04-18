package auth

import (
	"context"
	"testing"
)

func TestUserAddressContext_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	addr := "0x1234567890abcdef1234567890abcdef12345678"

	ctx = WithUserAddress(ctx, addr)
	got := UserAddressFrom(ctx)
	if got != addr {
		t.Errorf("UserAddressFrom = %q, want %q", got, addr)
	}
}

func TestUserAddressFrom_EmptyContext(t *testing.T) {
	t.Parallel()

	got := UserAddressFrom(context.Background())
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
