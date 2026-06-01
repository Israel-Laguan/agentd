package agentic

import (
	"context"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// noopHost is a no-op implementation of the Host interface for use in tests.
// Embed it in test host structs and override only the methods you need.
type noopHost struct{}

func (h *noopHost) FailHard(_ context.Context, _ models.Task, _ error) {}

func (h *noopHost) RunLegacyTask(_ context.Context, _ models.Task, _ models.Project, _ models.AgentProfile, _ bool) {
}

func (h *noopHost) RegisterCancel(_ string, _ context.CancelFunc) {}

func (h *noopHost) DeregisterCancel(_ string) {}

func (h *noopHost) HandleGatewayError(_ context.Context, _ models.Task, _ error) {}

func (h *noopHost) RecordTaskTokenUsage(_ context.Context, _ models.Task, _ int) {}

func (h *noopHost) CommitTextWithProfile(_ context.Context, _ models.Task, _ string, _ *models.AgentProfile) {
}

func (h *noopHost) DispatchToolWithHooks(
	_ context.Context,
	_, _, _ string,
	_ time.Time,
	call gateway.ToolCall,
	_ map[string]string,
	_ *agenttools.ToolExecutor,
	_ *agenthooks.HookChain,
	_ *capabilities.Registry,
	_ string,
) (agenttools.ToolResult, bool) {
	return agenttools.SuccessResult(call.ID, "noop", 0), false
}

func (h *noopHost) RunPreTaskElicitation(_ context.Context, task models.Task, _ models.Project) (models.Task, bool, error) {
	return task, false, nil
}

func (h *noopHost) Emit(_ context.Context, _ models.Task, _, _ string) {}

func (h *noopHost) AssembleAgenticSystemPrompt(_ context.Context, _ models.Task, _ models.Project, _ models.AgentProfile) []gateway.PromptMessage {
	return nil
}

func (h *noopHost) AssembleAgenticSystemPromptWithUserContent(_ context.Context, _ models.Task, _ models.Project, _ models.AgentProfile, _ string) []gateway.PromptMessage {
	return nil
}

func (h *noopHost) PrependReviewRejectionFeedback(_ context.Context, _ models.Task, messages []gateway.PromptMessage) ([]gateway.PromptMessage, string) {
	return messages, ""
}

func (h *noopHost) ApplyModelRouting(_ models.Task, profile models.AgentProfile, _ []gateway.PromptMessage, _ []gateway.ToolDefinition) models.AgentProfile {
	return profile
}

func (h *noopHost) ApplyTuning(req gateway.AIRequest, _ models.Task, _ models.AgentProfile, _ int) gateway.AIRequest {
	return req
}

func (h *noopHost) MountAgenticHooks(_ models.Project, _ models.AgentProfile) (*agenthooks.HookChain, *capabilities.Registry) {
	return agenthooks.NewHookChain(), nil
}

func (h *noopHost) AgenticToolsWithExtras(_ context.Context, _ *agenttools.ToolExecutor, _ *capabilities.Registry) ([]gateway.ToolDefinition, map[string]string) {
	return nil, nil
}

func (h *noopHost) FilterAgenticTools(tools []gateway.ToolDefinition, index map[string]string, _ models.Task, _ models.AgentProfile) ([]gateway.ToolDefinition, map[string]string) {
	return tools, index
}

func (h *noopHost) GeneratePlan(_ context.Context, _ models.Task, _ models.Project, _ *agentruntime.BudgetGuard) (*agentcontext.Plan, error) {
	return nil, nil
}

func (h *noopHost) InjectPlan(messages []gateway.PromptMessage, _ *agentcontext.Plan) []gateway.PromptMessage {
	return messages
}

func (h *noopHost) ShouldPlanWithBudget(_ models.Task, _ *agentruntime.BudgetGuard) bool {
	return false
}

func (h *noopHost) RepairOutputWithPlan(_ context.Context, _ models.Task, _ *agentcontext.Plan, content string, _ *agentruntime.BudgetGuard) (string, bool) {
	return content, false
}

func (h *noopHost) GenerateRespecifiedUserTurn(_ context.Context, _ models.Task, _ *agentcontext.Plan, _ []agentcontext.PlanStep, _ []gateway.PromptMessage, _ *agentcontext.ContextManager, _ *agentruntime.BudgetGuard) (string, error) {
	return "", nil
}

func (h *noopHost) RunSessionStart(_ context.Context, _ models.Task, _ models.Project, _ *agenthooks.HookChain) error {
	return nil
}

func (h *noopHost) TryExternalCapabilityRoute(_ context.Context, _ models.Task, _ models.Project, _ models.AgentProfile, _ *[]gateway.PromptMessage) (agentruntime.LoopResult, bool, error) {
	return agentruntime.LoopResult{}, false, nil
}

func (h *noopHost) RecordTurnSnapshot(_, _, _, _ string, _, _ int, _ []string, _ float64) {}

func (h *noopHost) RecordLoopResult(_ agentruntime.LoopResult) {}

func (h *noopHost) HandleGoalStalled(_ context.Context, _ models.Task, _ *agentcontext.GoalTracker) error {
	return nil
}
