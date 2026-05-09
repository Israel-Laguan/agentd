package routing

import (
	"context"
	"strings"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
)

const intentClassifierSystemPrompt = `You are the Frontdesk intent classifier for agentd. Classify the user message into exactly one intent. Return ONLY strict JSON with keys: intent, reason.

Intents:
- "plan_request": The user wants to build, create, implement, design, or plan new software work that agentd can execute as a local project. Examples: "Build me an API", "Create a web scraper", "I need a CLI tool".
- "out_of_scope": The request is impossible to fulfill as software, is not a deliverable project (illegal, harmful, get-rich-quick, gambling, surveillance, bypassing security, predicting markets, medical/legal advice as truth), is absurdly vague with no technical anchor ("make me a million dollars", "solve everything"), or is general chit-chat/jokes/weather with no buildable scope. Prefer this over plan_request when in doubt for non-software fantasies.
- "status_check": The user is asking about progress, status, what is running, what remains, how things are going, or wants a summary of current work. Examples: "What's the status?", "How are my tasks going?", "Any updates?", "Show me what's running".
- "ambiguous": The intent is unclear, mixes both planning and status, or does not match either category above. Examples: "Hello", "Can you help me?", "Tell me about agentd".

reason is one short sentence explaining your classification.`

var validIntents = map[string]bool{
	"plan_request": true,
	"out_of_scope": true,
	"status_check": true,
	"ambiguous":    true,
}

// ClassifyIntent implements spec.AIGateway.
func (r *Router) ClassifyIntent(ctx context.Context, userIntent string) (*spec.IntentAnalysis, error) {
	req := spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: intentClassifierSystemPrompt},
			{Role: "user", Content: userIntent},
		},
		Temperature: 0.1,
		JSONMode:    true,
		Role:        spec.RoleChat,
	}
	analysis, err := correction.GenerateJSON[spec.IntentAnalysis](ctx, r, req)
	if err != nil {
		return nil, err
	}
	analysis.Intent = strings.ToLower(analysis.Intent)
	if !validIntents[analysis.Intent] {
		analysis.Intent = "ambiguous"
	}
	return &analysis, nil
}
