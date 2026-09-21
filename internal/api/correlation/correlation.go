// Package correlation provides a context key for request correlation IDs.
// A correlation ID is generated at HTTP intake and carried through every
// subsystem (router, gateway, worker) as a structured slog field so a
// stalled or slow request can be traced end-to-end with one identifier.
package correlation

import "context"

type ctxKey struct{}

// WithID stores the correlation ID in the context.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext extracts the correlation ID from the context.
// Returns "" when no correlation ID is present.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}
