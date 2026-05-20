package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// PlanStep is one numbered step in a structured work plan.
type PlanStep struct {
	ID           string            `json:"id"`
	Action       string            `json:"action"`
	Inputs       map[string]string `json:"inputs"`
	OutputFormat string            `json:"output_format"`
}

// Plan is the JSON work plan produced before agentic execution.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// Validate checks semantic constraints on a parsed plan.
func (p *Plan) Validate() error {
	if p == nil || len(p.Steps) == 0 {
		return fmt.Errorf("plan must contain at least one step")
	}
	seen := make(map[string]struct{}, len(p.Steps))
	for i, step := range p.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("step %d: id is required", i+1)
		}
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %q: action is required", id)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate step id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// EstimateTaskComplexity scores how complex a task is for planning purposes.
func EstimateTaskComplexity(task models.Task) int {
	title := utf8.RuneCountInString(task.Title)
	desc := utf8.RuneCountInString(task.Description)
	newlines := strings.Count(task.Description, "\n")
	return title + desc + newlines*10
}

func (w *Worker) shouldPlan(task models.Task) bool {
	return w.planningCfg.ComplexityThreshold > 0 &&
		EstimateTaskComplexity(task) >= w.planningCfg.ComplexityThreshold
}

func (w *Worker) buildPlanContext(task models.Task, project models.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", project.ID)
	fmt.Fprintf(&b, "Task: %s\n", task.Title)
	if d := strings.TrimSpace(task.Description); d != "" {
		b.WriteString("Description:\n")
		b.WriteString(truncateRunes(d, w.planningCfg.PlanContextMaxChars))
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...[truncated]"
}

const planSystemPrompt = `You are a task planner. Output ONLY valid JSON matching this schema:
{"steps":[{"id":"step-id","action":"what to do","inputs":{"key":"value"},"output_format":"text|json|list"}]}
Rules:
- steps must be numbered logically (ids are stable slugs, e.g. analyze, implement, verify)
- output_format defaults to "text" when omitted
- keep the plan minimal and actionable`

func (w *Worker) buildPlanRequest(task models.Task, profile models.AgentProfile, planContext string) gateway.AIRequest {
	return gateway.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: planSystemPrompt},
			{Role: "user", Content: planContext},
		},
		JSONMode: true,
		AgentID:  task.AgentID,
		Role:     gateway.RoleMemory,
		TaskID:   task.ID,
		MaxTokens: 2000,
	}
}

func (w *Worker) generatePlan(
	ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile,
	budgetGuard *BudgetGuard,
) (*Plan, error) {
	if budgetGuard != nil {
		if err := budgetGuard.BeforeCall(); err != nil {
			return nil, err
		}
	}
	planContext := w.buildPlanContext(task, project)
	req := w.buildPlanRequest(task, profile, planContext)
	plan, err := gateway.GenerateJSON[Plan](ctx, w.gateway, req)
	if budgetGuard != nil {
		// GenerateJSON does not return token usage; best-effort skip AfterCall.
	}
	if err != nil {
		slog.Warn("agentic plan generation failed; continuing without plan",
			"task_id", task.ID, "error", err)
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		slog.Warn("agentic plan validation failed; continuing without plan",
			"task_id", task.ID, "error", err)
		return nil, err
	}
	return &plan, nil
}

func (w *Worker) injectPlan(messages []gateway.PromptMessage, plan *Plan) []gateway.PromptMessage {
	if plan == nil || len(messages) == 0 {
		return messages
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return messages
	}
	block := fmt.Sprintf(
		"\n\nWORK PLAN (execute each step; final answer MUST include one section per step using markers):\n"+
			"%s\n\n"+
			"For each step id, wrap output as:\n"+
			"<!-- step:<id> -->\n...content...\n<!-- /step:<id> -->\n",
		string(raw),
	)
	out := make([]gateway.PromptMessage, len(messages))
	copy(out, messages)
	for i := range out {
		if out[i].Role == "system" {
			out[i].Content += block
			return out
		}
	}
	return append([]gateway.PromptMessage{
		{Role: "system", Content: strings.TrimPrefix(block, "\n\n")},
	}, out...)
}
