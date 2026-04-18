package auth

import "context"

type contextKey string

const userAddressKey contextKey = "user_address"

// WithUserAddress stores the authenticated user's Ethereum address in the context.
func WithUserAddress(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, userAddressKey, address)
}

// UserAddressFrom retrieves the authenticated user's address from the context.
// Returns an empty string if no authenticated user is present.
func UserAddressFrom(ctx context.Context) string {
	if addr, ok := ctx.Value(userAddressKey).(string); ok {
		return addr
	}
	return ""
}
