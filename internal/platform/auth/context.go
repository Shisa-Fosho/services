package auth

import "context"

type contextKey string

const (
	userAddressKey  contextKey = "user_address"
	adminAddressKey contextKey = "admin_address"
)

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

// WithAdminAddress stores the address of an authenticated admin (one that has
// passed both JWT auth and the admin_wallets check) in the context. Handlers
// protected by RequireAdmin can read it back via AdminAddressFrom.
func WithAdminAddress(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, adminAddressKey, address)
}

// AdminAddressFrom retrieves the authenticated admin's address from the
// context. Returns an empty string if no admin is present.
func AdminAddressFrom(ctx context.Context) string {
	if addr, ok := ctx.Value(adminAddressKey).(string); ok {
		return addr
	}
	return ""
}
