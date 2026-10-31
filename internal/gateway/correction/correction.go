package correction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type attemptBudgetKey struct{}

type attemptBudgetState struct {
	budget AttemptBudget
	used   bool
	first  bool
}

// AttemptBudget is the per-call budget contract used while correcting JSON.
type AttemptBudget interface {
	BeforeCall() error
	AfterCall(int)
}

// WithAttemptBudget makes each provider attempt in the correction loop budget-aware.
func WithAttemptBudget(ctx context.Context, budget AttemptBudget) context.Context {
	state := &attemptBudgetState{budget: budget, first: true}
	ctx = context.WithValue(ctx, attemptBudgetKey{}, state)
	return ctx
}

// AttemptBudgetCharged reports whether provider attempts charged the context budget.
func AttemptBudgetCharged(ctx context.Context) bool {
	state, _ := ctx.Value(attemptBudgetKey{}).(*attemptBudgetState)
	if state == nil {
		return false
	}
	return state.used
}

func accumulateUsage(details *spec.UsageDetails, usage *spec.UsageDetails) {
	details.Model = ""
	if usage == nil {
		return
	}
	details.CachedTokens += usage.CachedTokens
	details.CacheWriteTokens += usage.CacheWriteTokens
	details.Model = usage.Model
}

// MaxJSONAttempts is the retry budget for JSON repair flows.
const MaxJSONAttempts = 3

// GenerateJSON unmarshals provider JSON with validation and self-correction prompts.
func GenerateJSON[T any](ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (T, error) {
	v, _, _, err := GenerateJSONWithUsage[T](ctx, gw, req)
	return v, err
}

// GenerateJSONWithUsage is like GenerateJSON but also returns the cumulative token
// usage across all provider calls in the retry loop (retries cost tokens too),
// plus cumulative prompt-cache usage details (cached_tokens / cache_write_tokens).
func GenerateJSONWithUsage[T any](ctx context.Context, gw spec.AIGateway, req spec.AIRequest) (T, int, spec.UsageDetails, error) {
	var zero T
	req.JSONMode = true
	var totalUsage int
	var details spec.UsageDetails
	var lastRaw string
	for attempt := 0; attempt < MaxJSONAttempts; attempt++ {
		if state, _ := ctx.Value(attemptBudgetKey{}).(*attemptBudgetState); state != nil {
			if !state.first {
				if err := state.budget.BeforeCall(); err != nil {
					return zero, totalUsage, details, err
				}
			} else {
				state.first = false
			}
		}
		if attempt > 0 {
			req.Attempt = spec.AttemptJSONRepair
		}
		resp, err := gw.Generate(ctx, req)
		if err != nil {
			return zero, totalUsage, details, err
		}
		state, _ := ctx.Value(attemptBudgetKey{}).(*attemptBudgetState)
		if state != nil {
			state.budget.AfterCall(resp.TokenUsage)
			state.used = true
		}
		totalUsage += resp.TokenUsage
		// Model names only this response's model, never a rejected earlier one.
		accumulateUsage(&details, resp.UsageDetails)
		lastRaw = resp.Content
		var out T
		if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
			if attempt == MaxJSONAttempts-1 {
				return zero, totalUsage, details, WrapInvalidJSONError(err, lastRaw)
			}
			req.Messages = append(req.Messages, promptAfterInvalid[T](err))
			continue
		}
		var validatable spec.Validatable
		if v, ok := any(out).(spec.Validatable); ok {
			validatable = v
		} else if v, ok := any(&out).(spec.Validatable); ok {
			validatable = v
		}
		if validatable != nil {
			if err := validatable.Validate(); err != nil {
				if attempt == MaxJSONAttempts-1 {
					return zero, totalUsage, details, WrapInvalidJSONError(err, lastRaw)
				}
				req.Messages = append(req.Messages, promptAfterInvalid[T](err))
				continue
			}
		}
		return out, totalUsage, details, nil
	}
	return zero, totalUsage, details, models.ErrInvalidJSONResponse
}

// PromptAfterInvalidJSON builds the corrective user message for invalid JSON.
func PromptAfterInvalidJSON(err error) spec.PromptMessage {
	return spec.PromptMessage{
		Role:    "user",
		Content: "Your previous response was invalid JSON. Error: " + err.Error() + ". Please output ONLY valid JSON.",
	}
}

// schemaHinter is implemented by target types that can describe their JSON
// shape; the description is appended to corrective retry messages.
type schemaHinter interface{ SchemaHint() string }

func promptAfterInvalid[T any](err error) spec.PromptMessage {
	msg := PromptAfterInvalidJSON(err)
	var zero T
	if h, ok := any(zero).(schemaHinter); ok && h.SchemaHint() != "" {
		msg.Content += "\n" + h.SchemaHint()
	}
	return msg
}

// WrapInvalidJSONError wraps a parse/validation error with optional raw model output.
func WrapInvalidJSONError(err error, raw string) error {
	return fmt.Errorf("%w: %v; raw response: %s", models.ErrInvalidJSONResponse, err, SummarizeRaw(raw))
}

// SummarizeRaw trims raw provider output for error messages.
func SummarizeRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	const maxRawErrorChars = 1000
	if len(raw) <= maxRawErrorChars {
		return raw
	}
	return raw[:maxRawErrorChars] + "...[truncated]"
}
