package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

var orderedListLinePattern = regexp.MustCompile(`^\d+\.\s`)

// stepMarkerPattern matches <!-- step:id --> and <!-- /step:id --> markers.
var stepMarkerPattern = regexp.MustCompile(`<!--\s*/?step:[^>]+-->\n?`)

func stripStepMarkers(output string) string {
	return strings.TrimSpace(stepMarkerPattern.ReplaceAllString(output, ""))
}

func isMarkdownListLine(trim string) bool {
	if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
		return true
	}
	return orderedListLinePattern.MatchString(trim)
}

func normalizeOutputFormat(format string) string {
	f := strings.ToLower(strings.TrimSpace(format))
	switch f {
	case "json", "list":
		return f
	default:
		return "text"
	}
}

func validateStepBody(format, body string) error {
	body = strings.TrimSpace(body)
	switch normalizeOutputFormat(format) {
	case "json":
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
	case "list":
		for _, line := range strings.Split(body, "\n") {
			trim := strings.TrimSpace(line)
			if isMarkdownListLine(trim) {
				return nil
			}
		}
		return fmt.Errorf("list must contain at least one markdown bullet line")
	default:
		if body == "" {
			return fmt.Errorf("section must be non-empty")
		}
	}
	return nil
}

// ValidateOutput returns plan steps whose sections are missing or invalid.
func ValidateOutput(output string, plan Plan) []PlanStep {
	var failing []PlanStep
	for _, step := range plan.Steps {
		body, ok := extractSection(output, step.ID)
		if !ok {
			failing = append(failing, step)
			continue
		}
		if err := validateStepBody(step.OutputFormat, body); err != nil {
			failing = append(failing, step)
		}
	}
	return failing
}

func extractSection(output, stepID string) (string, bool) {
	open := fmt.Sprintf("<!-- step:%s -->", stepID)
	close := fmt.Sprintf("<!-- /step:%s -->", stepID)
	start := strings.Index(output, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	end := strings.Index(output[start:], close)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(output[start : start+end]), true
}

// formatPlanOutputForCommit concatenates validated step bodies in plan order.
// Internal HTML comment markers are omitted from the committed task result.
func formatPlanOutputForCommit(output string, plan Plan) string {
	var parts []string
	for _, step := range plan.Steps {
		if body, ok := extractSection(output, step.ID); ok {
			body = strings.TrimSpace(body)
			if body != "" {
				parts = append(parts, body)
			}
		}
	}
	if len(parts) == 0 {
		if strings.Contains(output, "<!-- step:") {
			return stripStepMarkers(output)
		}
		return output
	}
	return strings.Join(parts, "\n\n")
}

// preparePlanCommitContent formats plan output for commit. When validation still
// fails after redo, it returns marker-stripped in-situ output instead of a
// partial join of only non-empty sections.
func preparePlanCommitContent(output string, plan Plan) string {
	if len(ValidateOutput(output, plan)) > 0 {
		return stripStepMarkers(output)
	}
	return formatPlanOutputForCommit(output, plan)
}

func replaceSection(output, stepID, newBody string) string {
	open := fmt.Sprintf("<!-- step:%s -->", stepID)
	close := fmt.Sprintf("<!-- /step:%s -->", stepID)
	start := strings.Index(output, open)
	if start < 0 {
		// Append missing section at end.
		return strings.TrimRight(output, "\n") + "\n\n" + open + "\n" + newBody + "\n" + close + "\n"
	}
	bodyStart := start + len(open)
	endRel := strings.Index(output[bodyStart:], close)
	if endRel < 0 {
		nextRel := strings.Index(output[bodyStart:], "<!-- step:")
		if nextRel < 0 {
			return output[:start] + open + "\n" + strings.TrimSpace(newBody) + "\n" + close + "\n"
		}
		next := bodyStart + nextRel
		return output[:bodyStart] + "\n" + strings.TrimSpace(newBody) + "\n" + close + "\n" + output[next:]
	}
	bodyEnd := bodyStart + endRel
	return output[:bodyStart] + "\n" + strings.TrimSpace(newBody) + "\n" + output[bodyEnd:]
}

func (w *Worker) buildRedoRequest(
	task models.Task, step PlanStep, errDesc, priorBody string,
) gateway.AIRequest {
	var b strings.Builder
	fmt.Fprintf(&b, "Repair ONLY step %q from the work plan.\n", step.ID)
	fmt.Fprintf(&b, "Action: %s\n", step.Action)
	if len(step.Inputs) > 0 {
		b.WriteString("Inputs:\n")
		for k, v := range step.Inputs {
			fmt.Fprintf(&b, "  %s: %s\n", k, v)
		}
	}
	fmt.Fprintf(&b, "Required output_format: %s\n", normalizeOutputFormat(step.OutputFormat))
	if errDesc != "" {
		fmt.Fprintf(&b, "Validation error: %s\n", errDesc)
	}
	if priorBody != "" {
		fmt.Fprintf(&b, "Previous attempt:\n%s\n", priorBody)
	}
	b.WriteString("\nOutput ONLY the section body (no markers). It will be inserted between step markers.")
	return gateway.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "You repair a single section of a multi-step agent response."},
			{Role: "user", Content: b.String()},
		},
		AgentID: task.AgentID,
		Role:    gateway.RoleMemory,
		TaskID:  task.ID,
	}
}

func (w *Worker) repairSection(
	ctx context.Context, task models.Task,
	step PlanStep, errDesc, priorBody string, budgetGuard *BudgetGuard,
) (string, error) {
	if budgetGuard != nil {
		if err := budgetGuard.BeforeCall(); err != nil {
			return "", err
		}
	}
	req := w.buildRedoRequest(task, step, errDesc, priorBody)
	resp, err := w.gateway.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	if budgetGuard != nil {
		budgetGuard.AfterCall(resp.TokenUsage)
	}
	return strings.TrimSpace(resp.Content), nil
}

func stepValidationError(step PlanStep, output string) string {
	body, ok := extractSection(output, step.ID)
	if !ok {
		return fmt.Sprintf("missing section markers for step %q", step.ID)
	}
	if err := validateStepBody(step.OutputFormat, body); err != nil {
		return err.Error()
	}
	return ""
}

func (w *Worker) repairOutputWithPlan(
	ctx context.Context, task models.Task, plan *Plan, output string,
	budgetGuard *BudgetGuard,
) string {
	if plan == nil || w.planningCfg.ComplexityThreshold <= 0 {
		return output
	}
	maxPasses := w.planningCfg.MaxRedoPasses
	passes := make(map[string]int)
	for {
		failing := ValidateOutput(output, *plan)
		if len(failing) == 0 {
			return output
		}
		progress := false
		for _, step := range failing {
			if passes[step.ID] >= maxPasses {
				continue
			}
			errDesc := stepValidationError(step, output)
			prior, _ := extractSection(output, step.ID)
			newBody, err := w.repairSection(ctx, task, step, errDesc, prior, budgetGuard)
			passes[step.ID]++
			if err != nil {
				continue
			}
			output = replaceSection(output, step.ID, newBody)
			progress = true
		}
		if !progress {
			return output
		}
	}
}
