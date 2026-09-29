package auth

import "context"

type contextKey struct{}

var adminUserKey = contextKey{}

// WithAdminUser attaches the authenticated AdminUser to the context.
func WithAdminUser(ctx context.Context, u *AdminUser) context.Context {
	return context.WithValue(ctx, adminUserKey, u)
}

// GetAdminUser retrieves the authenticated AdminUser from context.
func GetAdminUser(ctx context.Context) (*AdminUser, bool) {
	u, ok := ctx.Value(adminUserKey).(*AdminUser)
	return u, ok && u != nil
}
