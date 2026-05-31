package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// classifyPrecomputedToolResult classifies a hook-supplied result (cache hit,
// short-circuit) using the same rules as the normal execution path for that tool.
func ClassifyPrecomputedToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	switch toolName {
	case toolNameBash:
		// Hooks may inject sandbox transport JSON (e.g. {"FatalError":...}) for bash.
		return ClassifyRawResult(callID, raw, elapsedMs)
	case toolNameRead:
		return ClassifyPrecomputedReadResult(callID, raw, elapsedMs)
	case toolNameWrite:
		return ClassifyBuiltinToolResult(callID, toolName, raw, elapsedMs)
	case toolNameDelegate, toolNameDelegateParallel:
		return ClassifyDelegateRawResult(callID, raw, elapsedMs)
	default:
		return ClassifyCapabilityRawResult(callID, raw, elapsedMs)
	}
}

func classifyPrecomputedToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return ClassifyPrecomputedToolResult(callID, toolName, raw, elapsedMs)
}

// classifyBuiltinToolResult classifies built-in tool (bash, read, write) output.
// Read and write success paths return raw file bytes or {"success":true}; bash
// success returns raw stdout. Sandbox and jsonErrorf failures use toolErrorPrefix
// so arbitrary command stdout is never inferred from JSON shape alone.
func ClassifyBuiltinToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return ClassifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	switch toolName {
	case toolNameRead:
		// Success returns arbitrary file bytes; never infer failure from content shape.
		return SuccessResult(callID, raw, elapsedMs)
	case toolNameWrite:
		if isJSONErrorEnvelope(trimmed) {
			return ClassifyRawResult(callID, trimmed, elapsedMs)
		}
		return SuccessResult(callID, raw, elapsedMs)
	case toolNameBash:
		return SuccessResult(callID, raw, elapsedMs)
	default:
		return ClassifyRawResult(callID, raw, elapsedMs)
	}
}

func classifyBuiltinToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return ClassifyBuiltinToolResult(callID, toolName, raw, elapsedMs)
}

// classifyPrecomputedReadResult classifies hook/cache read results. Errors are
// distinguished by toolErrorPrefix (set by CacheStoreHook); file content that
// happens to be a single-key {"error":...} JSON object is treated as success,
// matching classifyBuiltinToolResult for live reads.
func ClassifyPrecomputedReadResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return ClassifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func classifyPrecomputedReadResult(callID, raw string, elapsedMs int64) ToolResult {
	return ClassifyPrecomputedReadResult(callID, raw, elapsedMs)
}

// classifyRawResult inspects a raw tool output string and returns a
// typed ToolResult. It uses the same JSON-envelope heuristics that
// parseToolExitCode relied on, so callers that previously examined the
// string can now switch on ToolResult.Status instead.
func ClassifyRawResult(callID, raw string, elapsedMs int64) ToolResult {
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

func classifyRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return ClassifyRawResult(callID, raw, elapsedMs)
}

// classifyCapabilityRawResult classifies MCP capability tool output without
// applying sandbox-style heuristics to arbitrary JSON payloads.
func ClassifyCapabilityRawResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return ClassifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	if isJSONErrorEnvelope(trimmed) {
		return ClassifyRawResult(callID, trimmed, elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func classifyCapabilityRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return ClassifyCapabilityRawResult(callID, raw, elapsedMs)
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
func ClassifyDelegateRawResult(callID, raw string, elapsedMs int64) ToolResult {
	trimmed := strings.TrimSpace(raw)
	if isToolErrorPayload(raw) {
		return ClassifyRawResult(callID, stripToolErrorPrefix(trimmed), elapsedMs)
	}
	if isJSONErrorEnvelope(trimmed) {
		return ClassifyRawResult(callID, trimmed, elapsedMs)
	}
	if strings.HasPrefix(trimmed, "[") {
		var batch []classifiedSubagentResult
		if err := json.Unmarshal([]byte(trimmed), &batch); err == nil && len(batch) > 0 {
			return classifySubagentBatch(callID, raw, batch, elapsedMs)
		}
	}
	var sr classifiedSubagentResult
	if err := json.Unmarshal([]byte(trimmed), &sr); err == nil && sr.Status != "" {
		return subagentResultToToolResult(callID, raw, sr, elapsedMs)
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func classifyDelegateRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return ClassifyDelegateRawResult(callID, raw, elapsedMs)
}

type classifiedSubagentResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

const (
	classifiedSubagentStatusSuccess = "success"
	classifiedSubagentStatusFailure = "failure"
	classifiedSubagentStatusTimeout = "timeout"
)

func classifySubagentBatch(callID, raw string, batch []classifiedSubagentResult, elapsedMs int64) ToolResult {
	for _, sr := range batch {
		if sr.Status != classifiedSubagentStatusSuccess {
			return subagentResultToToolResult(callID, raw, sr, elapsedMs)
		}
	}
	return SuccessResult(callID, raw, elapsedMs)
}

func subagentResultToToolResult(callID, raw string, sr classifiedSubagentResult, elapsedMs int64) ToolResult {
	switch sr.Status {
	case classifiedSubagentStatusSuccess:
		return SuccessResult(callID, raw, elapsedMs)
	case classifiedSubagentStatusTimeout:
		tr := TimeoutResult(callID, elapsedMs)
		tr.Content = raw
		return tr
	case classifiedSubagentStatusFailure:
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
