package spec

import (
	"context"
	"slices"
)

type sentCtxKey struct{}

// WithSentMessages asks the router to call callback with the messages it sends
// a provider, after the router's truncation, once for every provider attempt.
// A caller that must know what the model was shown, not only what it asked to
// send, uses it (B-089). A nil callback leaves ctx unchanged.
func WithSentMessages(ctx context.Context, callback func([]PromptMessage)) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, sentCtxKey{}, callback)
}

// SeeSentMessages hands messages to the function WithSentMessages attached to
// ctx, if any. It hands a copy, so the callback cannot change what is sent.
func SeeSentMessages(ctx context.Context, messages []PromptMessage) {
	see, ok := ctx.Value(sentCtxKey{}).(func([]PromptMessage))
	if !ok {
		return
	}
	sent := slices.Clone(messages)
	for i := range sent {
		sent[i].ToolCalls = slices.Clone(sent[i].ToolCalls)
	}
	see(sent)
}
