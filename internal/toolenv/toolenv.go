package toolenv

import "context"

type ctxKey struct{}

// With attaches per-call KEY=VALUE environment pairs to ctx for capability tools.
func With(ctx context.Context, env []string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	cp := append([]string(nil), env...)
	return context.WithValue(ctx, ctxKey{}, cp)
}

// From returns per-call env pairs previously attached with With.
func From(ctx context.Context) []string {
	v, _ := ctx.Value(ctxKey{}).([]string)
	return v
}
