package httpx

import "context"

type trustedOriginKey struct{}

// WithTrustedOrigin marks a request as coming from the configured web UI
// origin. The request guard sets it; handlers that would otherwise demand the
// materialize token accept the mark instead (decision D1, B-064).
func WithTrustedOrigin(ctx context.Context) context.Context {
	return context.WithValue(ctx, trustedOriginKey{}, true)
}

// FromTrustedOrigin reports whether WithTrustedOrigin marked ctx.
func FromTrustedOrigin(ctx context.Context) bool {
	trusted, _ := ctx.Value(trustedOriginKey{}).(bool)
	return trusted
}
