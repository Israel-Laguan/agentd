package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

// classifyPrecomputedToolResult classifies a hook-supplied result (cache hit,
// short-circuit) using the same rules as the normal execution path for that tool.
func classifyPrecomputedToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	switch toolName {
	case toolNameBash:
		// Hooks may inject sandbox transport JSON (e.g. {"FatalError":...}) for bash.
		return classifyRawResult(callID, raw, elapsedMs)
	case toolNameRead:
		return classifyPrecomputedReadResult(callID, raw, elapsedMs)
	case toolNameWrite:
		return classifyBuiltinToolResult(callID, toolName, raw, elapsedMs)
	case toolNameDelegate, toolNameDelegateParallel:
		return classifyDelegateRawResult(callID, raw, elapsedMs)
	default:
		return classifyCapabilityRawResult(callID, raw, elapsedMs)
	}
}

// classifyBuiltinToolResult classifies built-in tool (bash, read, write) output.
// Read and write success paths return raw file bytes or {"success":true}; bash
// success returns raw stdout. Sandbox and jsonErrorf failures use toolErrorPrefix
// so arbitrary command stdout is never inferred from JSON shape alone.
func classifyBuiltinToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return classifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	switch toolName {
	case toolNameRead:
		// Success returns arbitrary file bytes; never infer failure from content shape.
		return SuccessResult(callID, raw, elapsedMs)
	case toolNameWrite:
		if isJSONErrorEnvelope(trimmed) {
			return classifyRawResult(callID, trimmed, elapsedMs)
		}
		return SuccessResult(callID, raw, elapsedMs)
	case toolNameBash:
		return SuccessResult(callID, raw, elapsedMs)
	default:
		return classifyRawResult(callID, raw, elapsedMs)
	}
}

// classifyPrecomputedReadResult classifies hook/cache read results. It keeps the
// jsonErrorf envelope heuristic for cache entries stored before toolErrorPrefix
// existed, but live reads never use that heuristic (see classifyBuiltinToolResult).
func classifyPrecomputedReadResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return classifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	if isJSONErrorEnvelope(trimmed) {
		return classifyRawResult(callID, trimmed, elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

// classifyRawResult inspects a raw tool output string and returns a
// typed ToolResult. It uses the same JSON-envelope heuristics that
// parseToolExitCode relied on, so callers that previously examined the
// string can now switch on ToolResult.Status instead.
func classifyRawResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	var env struct {
		Success    *bool  `json:"Success"`
		ExitCode   *int   `json:"ExitCode"`
		Error      string `json:"error"`
		FatalError string `json:"FatalError"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal([]byte(trimmed), &env); err == nil {
		if env.FatalError != "" {
			tr := FatalResult(callID, env.FatalError, elapsedMs)
			tr.Content = raw
			return tr
		}
		if env.Status == "timeout" {
			return TimeoutResult(callID, elapsedMs)
		}
		if env.Error != "" {
			tr := NonRetryableErrorResult(callID, env.Error, "", elapsedMs)
			tr.Content = raw
			return tr
		}
		if env.Success != nil && !*env.Success {
			msg := "command failed"
			tr := NonRetryableErrorResult(callID, msg, "", elapsedMs)
			tr.Content = raw
			if env.ExitCode != nil {
				tr.Error.Message = fmt.Sprintf("command failed with exit code %d", *env.ExitCode)
				tr.ExitCode = *env.ExitCode
				tr.ExitCodeSet = true
			}
			return tr
		}
	} else {
		if strings.HasPrefix(trimmed, `{"FatalError"`) {
			return FatalResult(callID, raw, elapsedMs)
		}
		if strings.HasPrefix(trimmed, `{"error"`) {
			return NonRetryableErrorResult(callID, raw, "", elapsedMs)
		}
		if strings.HasPrefix(trimmed, `{"Success":false`) {
			return NonRetryableErrorResult(callID, raw, "", elapsedMs)
		}
	}
	return SuccessResult(callID, raw, elapsedMs)
}

// classifyCapabilityRawResult classifies MCP capability tool output without
// applying sandbox-style heuristics to arbitrary JSON payloads.
func classifyCapabilityRawResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return classifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	if isJSONErrorEnvelope(trimmed) {
		return classifyRawResult(callID, trimmed, elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

// isJSONErrorEnvelope reports whether raw is a single-key {"error":"..."} payload
// produced by jsonErrorf.
func isJSONErrorEnvelope(raw string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &m); err != nil {
		return false
	}
	_, hasError := m["error"]
	return hasError && len(m) == 1
}

// classifyDelegateRawResult classifies delegate and delegate_parallel string results
// without applying sandbox-style heuristics to SubagentResult JSON.
func classifyDelegateRawResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return classifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	if isJSONErrorEnvelope(trimmed) {
		return classifyRawResult(callID, trimmed, elapsedMs)
	}
	if strings.HasPrefix(trimmed, "[") {
		var batch []SubagentResult
		if err := json.Unmarshal([]byte(trimmed), &batch); err == nil && len(batch) > 0 {
			return classifySubagentBatch(callID, raw, batch, elapsedMs)
		}
	}
	var sr SubagentResult
	if err := json.Unmarshal([]byte(trimmed), &sr); err == nil && sr.Status != "" {
		return subagentResultToToolResult(callID, raw, sr, elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func classifySubagentBatch(callID, raw string, batch []SubagentResult, elapsedMs int64) ToolResult {
	for _, sr := range batch {
		if sr.Status != SubagentStatusSuccess {
			return subagentResultToToolResult(callID, raw, sr, elapsedMs)
		}
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func subagentResultToToolResult(callID, raw string, sr SubagentResult, elapsedMs int64) ToolResult {
	switch sr.Status {
	case SubagentStatusSuccess:
		return SuccessResult(callID, raw, elapsedMs)
	case SubagentStatusTimeout:
		tr := TimeoutResult(callID, elapsedMs)
		tr.Content = raw
		return tr
	case SubagentStatusFailure:
		msg := sr.Error
		if msg == "" {
			msg = "subagent " + string(sr.Status)
		}
		tr := NonRetryableErrorResult(callID, msg, "", elapsedMs)
		tr.Content = raw
		return tr
	default:
		return SuccessResult(callID, raw, elapsedMs)
	}
}
