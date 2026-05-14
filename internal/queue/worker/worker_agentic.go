package worker

import (
"context"
"log/slog"
"time"

"agentd/internal/capabilities"
"agentd/internal/config"
"agentd/internal/gateway"
"agentd/internal/models"
)

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
cancelCtx, cancel := context.WithCancel(ctx)
defer cancel()
w.registerCancel(task.ID, cancel)
defer w.deregisterCancel(task.ID)

// Create task-local ToolExecutor to avoid races with concurrent task executions
taskToolExecutor := NewToolExecutor(
w.sandbox,
project.WorkspacePath,
BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
w.sandboxWallTimeout,
)

taskHooks, taskCaps := w.mountScopedPlugins(project, profile)

messages := w.assembleAgenticSystemPrompt(ctx, task, project, profile)
tools, toolToAdapter := w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)

iterationGuard := NewIterationGuard(w.maxToolIterations)
budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
deadlineGuard := NewDeadlineGuard(cancelCtx)

// ContextManager is initialized lazily per task to handle its own cache/state
contextCfg := w.contextCfg
if contextCfg.RollingThresholdTurns <= 0 {
contextCfg.RollingThresholdTurns = config.DefaultRollingThresholdTurns
}
if contextCfg.KeepRecentTurns <= 0 {
contextCfg.KeepRecentTurns = config.DefaultKeepRecentTurns
}
if contextCfg.AnchorBudget <= 0 {
contextCfg.AnchorBudget = config.DefaultAnchorBudget
}
if contextCfg.WorkingBudget <= 0 {
contextCfg.WorkingBudget = config.DefaultWorkingBudget
}
if contextCfg.CompressedBudget <= 0 {
contextCfg.CompressedBudget = config.DefaultCompressedBudget
}

cm := NewContextManager(
contextCfg,
w.gateway,
task.AgentID,
task.ID,
)

for {
shouldContinue, err := w.processAgenticIteration(
cancelCtx, task, profile, &messages, tools, toolToAdapter, taskToolExecutor,
iterationGuard, budgetGuard, deadlineGuard, cm,
taskHooks, taskCaps,
)
if err != nil {
return
}
if !shouldContinue {
return
}
}
}

func (w *Worker) processAgenticIteration(
ctx context.Context, task models.Task, profile models.AgentProfile,
messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
toolToAdapter map[string]string, toolExecutor *ToolExecutor,
iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
deadlineGuard *DeadlineGuard, cm *ContextManager,
taskHooks *HookChain, _ *capabilities.Registry,
) (bool, error) {
if err := deadlineGuard.BeforeIteration(); err != nil {
w.handleGatewayError(ctx, task, err)
return false, err
}

if err := iterationGuard.BeforeIteration(); err != nil {
w.handleIterationExceeded(ctx, task)
return false, err
}

// Ingest human corrections from task comments. Use ContextManager.ShouldPollComments
// to avoid listing all comments on every iteration.
// Poll interval chosen to balance responsiveness and DB load.
const commentPollInterval = 5 * time.Second
if cm.ShouldPollComments(commentPollInterval) {
w.ingestHumanCorrections(ctx, task.ID, cm)
}

// Replace legacy truncator with ContextManager
prepared, err := cm.PrepareContext(ctx, *messages)
if err != nil {
w.handleGatewayError(ctx, task, err)
return false, err
}
*messages = prepared

if iterationGuard.ShouldInjectFinalMessage() {
*messages = append(*messages, iterationGuard.FinalMessage())
iterationGuard.ResetAllowFinal()
}

if err := budgetGuard.BeforeCall(); err != nil {
w.handleGatewayError(ctx, task, err)
return false, err
}

req := gateway.AIRequest{
Messages:    *messages,
Temperature: profile.Temperature,
Tools:       tools,
AgentID:     task.AgentID,
Role:        gateway.RoleWorker,
TaskID:      task.ID,
Provider:    profile.Provider,
Model:       profile.Model,
MaxTokens:   profile.MaxTokens,
}
req = w.applyTuning(req, task, profile)

resp, err := w.gateway.Generate(ctx, req)
if err != nil {
w.handleGatewayError(ctx, task, err)
return false, err
}

budgetGuard.AfterCall(resp.TokenUsage)

*messages = append(*messages, gateway.PromptMessage{
Role:      "assistant",
Content:   resp.Content,
ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
})

if len(resp.ToolCalls) == 0 {
w.commitText(ctx, task, resp.Content)
return false, nil
}

iterationGuard.AfterIteration(true)

for _, call := range resp.ToolCalls {
result := w.dispatchToolWithHooks(ctx, task.ID, task.ProjectID, call, toolToAdapter, toolExecutor, taskHooks)

if detected := cm.CheckToolResult(result); len(detected) > 0 {
slog.Info("auto-detected context corrections",
"task_id", task.ID,
"count", len(detected),
)
}

*messages = append(*messages, gateway.PromptMessage{
Role:       "tool",
ToolCallID: call.ID,
Content:    result,
})
}

return true, nil
}

func (w *Worker) agenticTools(ctx context.Context, toolExecutor *ToolExecutor) ([]gateway.ToolDefinition, map[string]string) {
tools := append([]gateway.ToolDefinition(nil), toolExecutor.Definitions()...)
tools = append(tools, DelegateToolDefinition())
if w.capabilities == nil {
return tools, nil
}
capabilityTools, toolToAdapter, err := w.capabilities.GetToolsAndAdapterIndex(ctx)
if err != nil {
slog.Warn("failed to get capability tools", "error", err)
return tools, nil
}
return append(tools, capabilityTools...), toolToAdapter
}
