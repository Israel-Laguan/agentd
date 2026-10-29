package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/gateway"
	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// EstimateTaskComplexity scores how complex a task is for planning purposes.
func EstimateTaskComplexity(task models.Task) int {
	title := utf8.RuneCountInString(task.Title)
	desc := utf8.RuneCountInString(task.Description)
	newlines := strings.Count(task.Description, "\n")
	return title + desc + newlines*10
}

// ShouldRunTiered reports whether the task should enter the tiered pipeline.
// Returns false when the tiered feature is disabled or the task scores below
// the complexity threshold (including threshold 0, which explicitly disables
// splitting). When disabled, the caller must use the existing one-shot path.
func (w *Worker) ShouldRunTiered(task models.Task) bool {
	if !w.tieredCfg.Enabled {
		return false
	}
	if w.tieredCfg.ComplexityThreshold <= 0 {
		return false
	}
	return EstimateTaskComplexity(task) >= w.tieredCfg.ComplexityThreshold
}

func (w *Worker) shouldPlan(task models.Task) bool {
	return w.planningCfg.ComplexityThreshold > 0 &&
		EstimateTaskComplexity(task) >= w.planningCfg.ComplexityThreshold
}

// planPhaseTokenReserve is a conservative upper bound on tokens one plan call may
// charge against the task budget (matches buildPlanRequest MaxTokens).
const planPhaseTokenReserve = 2000

// minExecutionTokenReserve is headroom we try to leave for the agentic loop when
// a per-task token budget is enabled.
const minExecutionTokenReserve = 1000

// shouldPlanWithBudget decides whether to run the plan phase. Plan generation uses
// the same BudgetGuard as execution, so both phases share one task token budget.
// When the budget is tight we skip planning so execution is less likely to stop
// immediately with LoopBudgetExhausted.
func (w *Worker) ShouldPlanWithBudget(task models.Task, budgetGuard *agentruntime.BudgetGuard) bool {
	if !w.shouldPlan(task) {
		return false
	}
	if w.tokenBudget <= 0 {
		return true
	}
	used := 0
	if budgetGuard != nil {
		used = budgetGuard.Usage()
	}
	remaining := w.tokenBudget - used
	need := planPhaseTokenReserve + minExecutionTokenReserve
	if remaining < need {
		slog.Info("skipping plan phase: insufficient token budget headroom for plan and execution",
			"task_id", task.ID, "used", used, "remaining", remaining, "need", need, "cap", w.tokenBudget)
		return false
	}
	return true
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

// buildPlanRequest is the work plan's one stage call. The contract carries the
// corrective retries, so a bad reply is repaired inside the call.
func (w *Worker) buildPlanRequest(task models.Task, planContext string) stage.StageRequest {
	return stage.StageRequest{
		Stage:   stage.StageTaskCreation,
		Purpose: gateway.PurposeExecWorkPlan,
		Context: []spec.PromptMessage{
			{Role: "system", Content: planSystemPrompt},
			{Role: "user", Content: planContext},
		},
		Contract:  stage.NewContract[agentcontext.Plan](),
		Role:      gateway.RoleMemory,
		AgentID:   task.AgentID,
		MaxTokens: planPhaseTokenReserve,
		Attrs:     spec.TaskAttrs{RequiresWorkspaceChange: workerBoolPtr(task.RequiresWorkspaceChange)},
	}
}

// GeneratePlan asks the work-plan stage for a plan. The budget guard is charged
// with what the call cost, a failed call included, but the tokens are not
// persisted as task usage.
func (w *Worker) GeneratePlan(
	ctx context.Context, task models.Task, project models.Project,
	budgetGuard *agentruntime.BudgetGuard,
) (*agentcontext.Plan, error) {
	req := w.buildPlanRequest(task, w.buildPlanContext(task, project))
	if budgetGuard != nil {
		if err := budgetGuard.BeforeCall(); err != nil {
			return nil, err
		}
		ctx = correction.WithAttemptBudget(ctx, budgetGuard)
	}
	res, err := w.taskCreationAgent().Call(gateway.WithTaskID(ctx, task.ID), req)
	if budgetGuard != nil && !correction.AttemptBudgetCharged(ctx) {
		budgetGuard.AfterCall(res.TokenUsage)
	}
	if err != nil {
		slog.Warn("agentic plan generation failed; continuing without plan",
			"task_id", task.ID, "error", err)
		return nil, err
	}
	if res.Outcome == stage.OutcomeNeedsHuman {
		return nil, handOffNeedsHuman(ctx, w.AskHuman, task, gateway.PurposeExecWorkPlan)
	}
	var plan agentcontext.Plan
	if err := json.Unmarshal(res.Output, &plan); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidJSONResponse, err)
	}
	return &plan, nil
}

func (w *Worker) InjectPlan(messages []gateway.PromptMessage, plan *agentcontext.Plan) []gateway.PromptMessage {
	if plan == nil {
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
	if len(out) == 0 {
		return []gateway.PromptMessage{
			{Role: "system", Content: strings.TrimPrefix(block, "\n\n")},
		}
	}
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
