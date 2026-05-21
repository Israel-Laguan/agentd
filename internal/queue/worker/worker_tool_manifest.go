package worker

import (
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// filterAgenticTools applies per-task tool manifest filtering when enabled.
func (w *Worker) filterAgenticTools(
	tools []gateway.ToolDefinition,
	index map[string]string,
	task models.Task,
	profile models.AgentProfile,
) ([]gateway.ToolDefinition, map[string]string) {
	if w.toolManifest == nil {
		return tools, index
	}
	return w.toolManifest.Filter(tools, index, task, profile)
}
