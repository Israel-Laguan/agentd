package correction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

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
		resp, err := gw.Generate(ctx, req)
		if err != nil {
			return zero, totalUsage, details, err
		}
		totalUsage += resp.TokenUsage
		details.CachedTokens += resp.UsageDetails.CachedTokens
		details.CacheWriteTokens += resp.UsageDetails.CacheWriteTokens
		lastRaw = resp.Content
		var out T
		if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
			if attempt == MaxJSONAttempts-1 {
				return zero, totalUsage, details, WrapInvalidJSONError(err, lastRaw)
			}
			req.Messages = append(req.Messages, PromptAfterInvalidJSON(err))
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
				req.Messages = append(req.Messages, PromptAfterInvalidJSON(err))
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

// EnforcePhaseCap trims an oversized DraftPlan to the configured phase cap.
func EnforcePhaseCap(plan models.DraftPlan, maxTasksPerPhase int) models.DraftPlan {
	if maxTasksPerPhase <= 0 || len(plan.Tasks) <= maxTasksPerPhase {
		return plan
	}
	if maxTasksPerPhase == 1 {
		plan.Tasks = []models.DraftTask{phaseContinuationTask(2, plan.Tasks)}
		return plan
	}
	remaining := append([]models.DraftTask(nil), plan.Tasks[maxTasksPerPhase-1:]...)
	plan.Tasks = append([]models.DraftTask(nil), plan.Tasks[:maxTasksPerPhase-1]...)
	plan.Tasks = append(plan.Tasks, phaseContinuationTask(2, remaining))
	return plan
}

func phaseContinuationTask(phase int, remaining []models.DraftTask) models.DraftTask {
	return models.DraftTask{
		Title:       fmt.Sprintf("Plan Phase %d", phase),
		Description: phaseContinuationDescription(remaining),
		Assignee:    models.TaskAssigneeSystem,
	}
}

func phaseContinuationDescription(remaining []models.DraftTask) string {
	if len(remaining) == 0 {
		return "Continue planning the remaining work for the project."
	}
	var b strings.Builder
	b.WriteString("Continue planning the remaining work. Start from these deferred tasks and any related follow-up work:\n")
	for _, task := range remaining {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = strings.TrimSpace(task.ID())
		}
		if title == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(title)
		description := strings.TrimSpace(task.Description)
		if description != "" {
			b.WriteString(": ")
			b.WriteString(description)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
