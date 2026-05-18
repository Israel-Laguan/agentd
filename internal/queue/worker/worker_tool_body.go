package worker

import (
	"context"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
)

func (w *Worker) runToolBody(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor, scopedCapabilities *capabilities.Registry) ToolResult {
	start := time.Now()
	switch call.Function.Name {
	case toolNameBash, toolNameRead, toolNameWrite:
		raw := toolExecutor.Execute(ctx, call)
		return classifyBuiltinToolResult(call.ID, call.Function.Name, raw, time.Since(start).Milliseconds())
	case toolNameDelegate:
		raw := w.executeDelegateWithCapabilities(ctx, call, toolExecutor, scopedCapabilities)
		return classifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	case toolNameDelegateParallel:
		raw := w.executeDelegateParallel(ctx, call, toolExecutor, scopedCapabilities)
		return classifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	default:
		raw := executeCapabilityTool(ctx, call, toolToAdapter, w.capabilities, scopedCapabilities)
		return classifyCapabilityRawResult(call.ID, raw, time.Since(start).Milliseconds())
	}
}
