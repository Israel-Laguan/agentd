package context

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var orderedListLinePattern = regexp.MustCompile(`^\d+\.\s`)

// stepMarkerPattern matches <!-- step:id --> and <!-- /step:id --> markers.
var stepMarkerPattern = regexp.MustCompile(`<!--\s*/?step:[^>]+-->\n?`)

func StripStepMarkers(output string) string {
	return strings.TrimSpace(stepMarkerPattern.ReplaceAllString(output, ""))
}

func isMarkdownListLine(trim string) bool {
	if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
		return true
	}
	return orderedListLinePattern.MatchString(trim)
}

func NormalizeOutputFormat(format string) string {
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
	switch NormalizeOutputFormat(format) {
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
		body, ok := ExtractSection(output, step.ID)
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

func ExtractSection(output, stepID string) (string, bool) {
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

// FormatPlanOutputForCommit concatenates validated step bodies in plan order.
// Internal HTML comment markers are omitted from the committed task result.
func FormatPlanOutputForCommit(output string, plan Plan) string {
	var parts []string
	for _, step := range plan.Steps {
		if body, ok := ExtractSection(output, step.ID); ok {
			body = strings.TrimSpace(body)
			if body != "" {
				parts = append(parts, body)
			}
		}
	}
	if len(parts) == 0 {
		if strings.Contains(output, "<!-- step:") {
			return StripStepMarkers(output)
		}
		return output
	}
	return strings.Join(parts, "\n\n")
}

// PreparePlanCommitContent formats plan output for commit. When validation still
// fails after redo, it returns marker-stripped in-situ output instead of a
// partial join of only non-empty sections.
func PreparePlanCommitContent(output string, plan Plan) string {
	if len(ValidateOutput(output, plan)) > 0 {
		return StripStepMarkers(output)
	}
	return FormatPlanOutputForCommit(output, plan)
}

func ReplaceSection(output, stepID, newBody string) string {
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

func StepValidationError(step PlanStep, output string) string {
	body, ok := ExtractSection(output, step.ID)
	if !ok {
		return fmt.Sprintf("missing section markers for step %q", step.ID)
	}
	if err := validateStepBody(step.OutputFormat, body); err != nil {
		return err.Error()
	}
	return ""
}
