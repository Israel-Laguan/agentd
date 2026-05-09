package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

var _ spec.ContractAdapter = (*Router)(nil)

// GenerateText adapts the proposal contract to the existing provider router.
func (r *Router) GenerateText(ctx context.Context, prompt string, limit int) (string, error) {
	resp, err := r.Generate(ctx, spec.AIRequest{
		Messages:    []spec.PromptMessage{{Role: "user", Content: prompt}},
		MaxTokens:   limit,
		Temperature: 0.2,
		Role:        spec.RoleWorker,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// GenerateStructuredJSON produces JSON and unmarshals into target.
func (r *Router) GenerateStructuredJSON(ctx context.Context, prompt string, target interface{}) error {
	if target == nil {
		return fmt.Errorf("target is required")
	}
	req := spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "Output ONLY valid JSON matching the requested schema."},
			{Role: "user", Content: prompt},
		},
		JSONMode:    true,
		Temperature: 0.2,
		Role:        spec.RoleChat,
	}
	var lastRaw string
	for attempt := 0; attempt < correction.MaxJSONAttempts; attempt++ {
		resp, err := r.Generate(ctx, req)
		if err != nil {
			return err
		}
		lastRaw = resp.Content
		if err := json.Unmarshal([]byte(resp.Content), target); err != nil {
			if attempt == correction.MaxJSONAttempts-1 {
				return correction.WrapInvalidJSONError(err, lastRaw)
			}
			req.Messages = append(req.Messages, correction.PromptAfterInvalidJSON(err))
			continue
		}
		if v, ok := target.(spec.Validatable); ok {
			if err := v.Validate(); err != nil {
				if attempt == correction.MaxJSONAttempts-1 {
					return correction.WrapInvalidJSONError(err, lastRaw)
				}
				req.Messages = append(req.Messages, correction.PromptAfterInvalidJSON(err))
				continue
			}
		}
		return nil
	}
	return models.ErrInvalidJSONResponse
}

// TruncateToBudget applies a conservative token-to-char approximation.
func (r *Router) TruncateToBudget(input string, maxTokens int) string {
	if maxTokens <= 0 {
		return input
	}
	maxChars := maxTokens * 4
	if maxChars <= 0 || len(input) <= maxChars {
		return input
	}
	return input[:maxChars]
}
