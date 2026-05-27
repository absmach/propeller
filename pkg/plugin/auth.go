package plugin

import "context"

type authKey struct{}

type systemKey struct{}

const SystemUserID = "system"

// ContextWithAuth stores an AuthContext in ctx.
func ContextWithAuth(ctx context.Context, auth AuthContext) context.Context {
	return context.WithValue(ctx, authKey{}, auth)
}

// AuthFromContext retrieves the AuthContext stored by ContextWithAuth.
// Returns a zero-value AuthContext when none is present.
func AuthFromContext(ctx context.Context) AuthContext {
	v, _ := ctx.Value(authKey{}).(AuthContext)

	return v
}

// ContextWithSystem marks ctx as originating from an internal system trigger
// (cron scheduler, workflow coordinator). The returned context carries a
// system AuthContext so plugins can identify the caller.
func ContextWithSystem(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, systemKey{}, true)

	return ContextWithAuth(ctx, AuthContext{UserID: SystemUserID})
}

// IsSystemInitiated reports whether ctx was created by ContextWithSystem.
func IsSystemInitiated(ctx context.Context) bool {
	v, _ := ctx.Value(systemKey{}).(bool)

	return v
}
