package spec

import "context"

type sentCtxKey struct{}

// WithSentMessages has the router hand see the messages it sends a provider, on
// every attempt, after its own cut. A caller that must know what the model was
// shown, not only what it asked to send, passes one (B-089). A nil see leaves
// ctx as it is, so the router never calls a nil function.
func WithSentMessages(ctx context.Context, see func([]PromptMessage)) context.Context {
	if see == nil {
		return ctx
	}
	return context.WithValue(ctx, sentCtxKey{}, see)
}

// SeeSentMessages hands messages to the function WithSentMessages attached to
// ctx, if any.
func SeeSentMessages(ctx context.Context, messages []PromptMessage) {
	if see, ok := ctx.Value(sentCtxKey{}).(func([]PromptMessage)); ok {
		see(messages)
	}
}
