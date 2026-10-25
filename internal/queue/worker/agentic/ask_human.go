package agentic

import (
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

// isAskHumanBlock reports whether call is an ask_human call that blocked the
// task. ask_human runs through tool dispatch like any tool, so hooks can veto
// it; only a successful call blocked the task, and then the loop stops.
func isAskHumanBlock(call gateway.ToolCall, tr agenttools.ToolResult) bool {
	return call.Function.Name == agenttools.ToolNameAskHuman && tr.Status == agenttools.ToolStatusSuccess
}
