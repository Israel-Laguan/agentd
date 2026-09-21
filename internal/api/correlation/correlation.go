// Package correlation provides a context key for request correlation IDs.
// A correlation ID is generated at HTTP intake and carried through every
// subsystem (router, gateway, worker) as a structured slog field so a
// stalled or slow request can be traced end-to-end with one identifier.
package correlation

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

const correlationIDField = "correlation_id"

// WithID stores the correlation ID in the context.
func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext extracts the correlation ID from the context.
// Returns "" when no correlation ID is present.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Attr returns a slog.Attr carrying the correlation ID from ctx, suitable
// for inlining into existing slog calls:
//
//	slog.DebugContext(ctx, "event", correlation.Attr(ctx), "detail", x)
//
// When no correlation ID is present the field is omitted so logs without an
// HTTP intake (e.g. background workers) stay low-noise.
func Attr(ctx context.Context) slog.Attr {
	id := FromContext(ctx)
	if id == "" {
		return slog.Attr{}
	}
	return slog.String(correlationIDField, id)
}

// Logger returns a *slog.Logger that automatically records the correlation ID
// carried by ctx on every emitted record. When ctx has no correlation ID the
// returned logger is the default slog logger. Use Logger(ctx) instead of the
// package-level slog.DebugContext(ctx, ...) helpers when you want the
// correlation ID attached without repeating the field at every call site:
//
//	correlation.Logger(ctx).DebugContext(ctx, "agentic: llm call start", ...)
func Logger(ctx context.Context) *slog.Logger {
	id := FromContext(ctx)
	if id == "" {
		return slog.Default()
	}
	return slog.Default().With(slog.String(correlationIDField, id))
}
