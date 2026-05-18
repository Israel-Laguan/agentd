package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolStatus classifies the outcome of a tool execution.
type ToolStatus int

const (
	// ToolStatusSuccess indicates the tool executed successfully.
	ToolStatusSuccess ToolStatus = iota
	// ToolStatusError indicates a general (potentially retryable) error.
	ToolStatusError
	// ToolStatusTimeout indicates the tool exceeded its time budget.
	ToolStatusTimeout
	// ToolStatusVetoed indicates the tool call was blocked by policy.
	ToolStatusVetoed
	// ToolStatusFatal indicates an unrecoverable execution failure.
	ToolStatusFatal
)

// String returns the human-readable label for a ToolStatus.
func (s ToolStatus) String() string {
	switch s {
	case ToolStatusSuccess:
		return "success"
	case ToolStatusError:
		return "error"
	case ToolStatusTimeout:
		return "timeout"
	case ToolStatusVetoed:
		return "vetoed"
	case ToolStatusFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// ToolError carries structured error information from a tool execution.
type ToolError struct {
	Message   string
	Code      string
	Retryable bool
}

// ToolResult is the structured outcome of a single tool execution.
type ToolResult struct {
	CallID    string
	Status    ToolStatus
	Content   string
	Error     *ToolError
	Retryable bool
	ElapsedMs int64
}

// ForContext formats the result for injection into the model's context
// with an appropriate prefix based on the status.
func (r ToolResult) ForContext() string {
	switch r.Status {
	case ToolStatusSuccess:
		return r.Content
	case ToolStatusVetoed:
		return fmt.Sprintf("[POLICY] Tool call blocked: %s", r.Content)
	case ToolStatusTimeout:
		return fmt.Sprintf("[TIMEOUT] Tool did not respond within %dms", r.ElapsedMs)
	case ToolStatusFatal:
		if r.Content != "" {
			return fmt.Sprintf("[FATAL] Tool execution failed unrecoverably: %s", r.Content)
		}
		return "[FATAL] Tool execution failed unrecoverably"
	case ToolStatusError:
		if r.Retryable {
			return fmt.Sprintf("[RETRYABLE ERROR] %s", r.Content)
		}
		return fmt.Sprintf("[ERROR] %s", r.Content)
	default:
		return r.Content
	}
}

// SuccessResult builds a ToolResult for a successful execution.
func SuccessResult(callID, content string, elapsedMs int64) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusSuccess,
		Content:   content,
		Retryable: false,
		ElapsedMs: elapsedMs,
	}
}

// ErrorResult builds a ToolResult for a retryable error.
func ErrorResult(callID, message, code string, elapsedMs int64) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusError,
		Content:   message,
		Error:     &ToolError{Message: message, Code: code, Retryable: true},
		Retryable: true,
		ElapsedMs: elapsedMs,
	}
}

// NonRetryableErrorResult builds a ToolResult for a non-retryable error.
func NonRetryableErrorResult(callID, message, code string, elapsedMs int64) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusError,
		Content:   message,
		Error:     &ToolError{Message: message, Code: code, Retryable: false},
		Retryable: false,
		ElapsedMs: elapsedMs,
	}
}

// TimeoutResult builds a ToolResult for a timed-out execution.
func TimeoutResult(callID string, elapsedMs int64) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusTimeout,
		Content:   fmt.Sprintf("tool did not respond within %dms", elapsedMs),
		Error:     &ToolError{Message: fmt.Sprintf("tool did not respond within %dms", elapsedMs), Code: "TIMEOUT", Retryable: true},
		Retryable: true,
		ElapsedMs: elapsedMs,
	}
}

// VetoedResult builds a ToolResult for a policy-vetoed tool call.
func VetoedResult(callID, reason string) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusVetoed,
		Content:   reason,
		Error:     &ToolError{Message: reason, Code: "VETOED", Retryable: false},
		Retryable: false,
	}
}

// FatalResult builds a ToolResult for an unrecoverable failure.
func FatalResult(callID, message string, elapsedMs int64) ToolResult {
	return ToolResult{
		CallID:    callID,
		Status:    ToolStatusFatal,
		Content:   message,
		Error:     &ToolError{Message: message, Code: "FATAL", Retryable: false},
		Retryable: false,
		ElapsedMs: elapsedMs,
	}
}

// classifyRawResult inspects a raw tool output string and returns a
// typed ToolResult. It uses the same JSON-envelope heuristics that
// parseToolExitCode relied on, so callers that previously examined the
// string can now switch on ToolResult.Status instead.
func classifyRawResult(callID, raw string, elapsedMs int64) ToolResult {
	var env struct {
		Success    *bool  `json:"Success"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"error"`
		FatalError string `json:"FatalError"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err == nil {
		if env.FatalError != "" {
			return FatalResult(callID, env.FatalError, elapsedMs)
		}
		if env.Status == "timeout" {
			return TimeoutResult(callID, elapsedMs)
		}
		if env.Error != "" {
			return NonRetryableErrorResult(callID, env.Error, "", elapsedMs)
		}
		if env.Success != nil && !*env.Success {
			msg := fmt.Sprintf("command failed with exit code %d", env.ExitCode)
			return NonRetryableErrorResult(callID, msg, "", elapsedMs)
		}
	} else {
		if strings.HasPrefix(raw, `{"FatalError"`) {
			return FatalResult(callID, raw, elapsedMs)
		}
		if strings.HasPrefix(raw, `{"error"`) {
			return NonRetryableErrorResult(callID, raw, "", elapsedMs)
		}
		if strings.HasPrefix(raw, `{"Success":false`) {
			return NonRetryableErrorResult(callID, raw, "", elapsedMs)
		}
	}
	return SuccessResult(callID, raw, elapsedMs)
}
