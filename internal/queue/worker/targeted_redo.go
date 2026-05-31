package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

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

func (w *Worker) repairOutputWithPlan(
	ctx context.Context, task models.Task, plan *Plan, output string,
	budgetGuard *BudgetGuard,
) (string, bool) {
	if plan == nil || w.planningCfg.ComplexityThreshold <= 0 {
		return output, false
	}
	maxPasses := w.planningCfg.MaxRedoPasses
	passes := make(map[string]int)
	consecutiveNoProgress := 0
	for {
		failing := ValidateOutput(output, *plan)
		if len(failing) == 0 {
			return output, false
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
			consecutiveNoProgress++
			if consecutiveNoProgress > 2 {
				return output, true
			}
			continue
		}
		consecutiveNoProgress = 0
	}
}
