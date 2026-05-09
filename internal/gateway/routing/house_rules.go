package routing

import (
	"context"
	"strings"

	"agentd/internal/gateway/spec"
)

type ctxKeyHouseRules struct{}

// WithHouseRules attaches global house rules to ctx for Router.Generate and
// Router-backed JSON flows. Empty rules are a no-op.
func WithHouseRules(ctx context.Context, rules string) context.Context {
	rules = strings.TrimSpace(rules)
	if rules == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyHouseRules{}, rules)
}

// HouseRulesFromContext returns rules previously set with WithHouseRules.
func HouseRulesFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyHouseRules{}).(string)
	return strings.TrimSpace(v)
}

const houseRulesPromptPrefix = "House rules (apply to this request):\n"

func mergeHouseRulesIntoMessages(msgs []spec.PromptMessage, rules string) []spec.PromptMessage {
	rules = strings.TrimSpace(rules)
	if rules == "" {
		return msgs
	}
	prefix := houseRulesPromptPrefix + rules + "\n\n"
	out := append([]spec.PromptMessage(nil), msgs...)
	if len(out) > 0 && out[0].Role == "system" {
		out[0].Content = prefix + out[0].Content
		return out
	}
	return append([]spec.PromptMessage{{Role: "system", Content: strings.TrimSpace(prefix)}}, out...)
}
