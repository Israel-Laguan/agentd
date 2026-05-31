package worker

import agenttools "agentd/internal/agent/tools"

func classifyPrecomputedToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyPrecomputedToolResult(callID, toolName, raw, elapsedMs)
}

func classifyBuiltinToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyBuiltinToolResult(callID, toolName, raw, elapsedMs)
}

func classifyPrecomputedReadResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyPrecomputedReadResult(callID, raw, elapsedMs)
}

func classifyRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyRawResult(callID, raw, elapsedMs)
}

func classifyCapabilityRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyCapabilityRawResult(callID, raw, elapsedMs)
}

func classifyDelegateRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyDelegateRawResult(callID, raw, elapsedMs)
}
