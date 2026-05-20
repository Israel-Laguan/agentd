package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func (w *Worker) buildTurnRespecRequest(
	task models.Task,
	plan *Plan,
	failing []PlanStep,
	originalUserContent string,
) gateway.AIRequest {
	var b strings.Builder
	b.WriteString("Revise the user task prompt so a multi-step agent can complete all plan steps.\n")
	if originalUserContent != "" {
		fmt.Fprintf(&b, "Original user turn:\n%s\n\n", originalUserContent)
	}
	if plan != nil {
		b.WriteString("Work plan:\n")
		for _, step := range plan.Steps {
			fmt.Fprintf(&b, "- %s: %s (format: %s)\n", step.ID, step.Action, normalizeOutputFormat(step.OutputFormat))
		}
	}
	if len(failing) > 0 {
		b.WriteString("\nSteps still failing validation:\n")
		for _, step := range failing {
			fmt.Fprintf(&b, "- %s: %s\n", step.ID, step.Action)
		}
	}
	b.WriteString("\nOutput ONLY the revised user message text (no markdown fences, no role labels).")
	return gateway.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "You rewrite user task prompts for autonomous coding agents."},
			{Role: "user", Content: b.String()},
		},
		AgentID: task.AgentID,
		Role:    gateway.RoleMemory,
		TaskID:  task.ID,
	}
}

func anchorUserContent(messages []gateway.PromptMessage, cm *ContextManager) string {
	if cm == nil {
		cm = &ContextManager{}
	}
	anchor, _ := cm.partitionAnchor(messages)
	for i := len(anchor) - 1; i >= 0; i-- {
		if anchor[i].Role == "user" {
			return anchor[i].Content
		}
	}
	return ""
}

func (w *Worker) generateRespecifiedUserTurn(
	ctx context.Context,
	task models.Task,
	plan *Plan,
	failing []PlanStep,
	messages []gateway.PromptMessage,
	cm *ContextManager,
	budgetGuard *BudgetGuard,
) (string, error) {
	if budgetGuard != nil {
		if err := budgetGuard.BeforeCall(); err != nil {
			return "", err
		}
	}
	original := anchorUserContent(messages, cm)
	req := w.buildTurnRespecRequest(task, plan, failing, original)
	resp, err := w.gateway.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	if budgetGuard != nil {
		budgetGuard.AfterCall(resp.TokenUsage)
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("respecify user turn: empty gateway response")
	}
	return content, nil
}
