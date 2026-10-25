package worker

import (
	"context"
	"log/slog"

	agentsubagent "agentd/internal/agent/subagent"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

func (w *Worker) agenticTools(ctx context.Context, toolExecutor *agenttools.ToolExecutor) ([]gateway.ToolDefinition, map[string]string) {
	tools := append([]gateway.ToolDefinition(nil), toolExecutor.Definitions()...)
	// ask_human is offered here, not in the executor's definitions, so a
	// subagent (which builds its tools from the executor) never gets it: a
	// subagent cannot suspend the task.
	tools = append(tools, agentsubagent.DelegateToolDefinition(), agentsubagent.DelegateParallelToolDefinition(), agenttools.AskHumanToolDefinition())
	if w.capabilities == nil {
		gateway.SortTools(tools)
		return tools, nil
	}
	capabilityTools, toolToAdapter, err := w.capabilities.GetToolsAndAdapterIndex(ctx)
	if err != nil {
		slog.Warn("failed to get capability tools", "error", err)
		gateway.SortTools(tools)
		return tools, nil
	}
	tools = append(tools, capabilityTools...)
	gateway.SortTools(tools)
	return tools, toolToAdapter
}
