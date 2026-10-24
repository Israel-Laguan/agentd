package tools

import (
	"encoding/json"
	"fmt"

	"agentd/internal/sandbox"
)

// ToolErrorCodeSandboxUnavailable tags a fatal tool result whose command was
// refused because a required jail is unavailable (B-051/B-052). The worker
// fails such a task terminally instead of retrying it.
const ToolErrorCodeSandboxUnavailable = "SANDBOX_UNAVAILABLE"

// ToolErrorCodeNoProgress tags a loop stop after the same tool call returned
// the same result DefaultNoProgressRepeats times (B-081). Retrying would
// repeat the cycle, so the worker hands the task to a human instead.
const ToolErrorCodeNoProgress = "NO_PROGRESS"

// ToolErrorCodeUnverified tags a text-only finish the worker cannot verify, such
// as one with an unmet success criterion or open breakdown subtasks, so the
// worker sends the task to a human instead of marking it COMPLETED.
const ToolErrorCodeUnverified = "UNVERIFIED_COMPLETION"

// ToolErrorCodeUnverifiedCommand tags a text-only finish made right after a
// failed shell command (B-082). The model can fix the command, so the worker
// retries the task with the failure as feedback (T-055b gap 3).
const ToolErrorCodeUnverifiedCommand = "UNVERIFIED_COMMAND"

// SandboxUnavailableJSON reports a refusal by an unavailable required jail as a
// fatal, tagged tool result: the model cannot fix a missing jail, so the
// agentic loop must stop instead of feeding the error back for another try.
func SandboxUnavailableJSON(err error) string {
	payload, marshalErr := json.Marshal(map[string]string{
		"FatalError": err.Error(),
		"Code":       ToolErrorCodeSandboxUnavailable,
	})
	if marshalErr != nil {
		return toolErrorPrefix + `{"FatalError":"sandbox backend unavailable"}`
	}
	return toolErrorPrefix + string(payload)
}

func SandboxFailureJSON(result sandbox.Result) string {
	payload, err := json.Marshal(map[string]any{
		"Success":  false,
		"ExitCode": result.ExitCode,
		"Stdout":   result.Stdout,
		"Stderr":   result.Stderr,
	})
	if err != nil {
		return toolErrorPrefix + `{"error":"failed to encode sandbox failure payload"}`
	}
	return toolErrorPrefix + string(payload)
}

// SandboxTimeoutJSON reports a command the sandbox stopped for lack of output.
// The output it printed rides along, so a prompt the command waited on can be
// recognised (PROMPT_DETECTED).
func SandboxTimeoutJSON(result sandbox.Result, err error) string {
	payload, marshalErr := json.Marshal(map[string]any{
		"error":    err.Error(),
		"TimedOut": true,
		"ExitCode": result.ExitCode,
		"Stdout":   result.Stdout,
		"Stderr":   result.Stderr,
	})
	if marshalErr != nil {
		return toolErrorPrefix + `{"error":"sandbox timed out"}`
	}
	return toolErrorPrefix + string(payload)
}

func JSONErrorf(format string, args ...any) string {
	payload, err := json.Marshal(map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
	if err != nil {
		return toolErrorPrefix + `{"error":"failed to encode error payload"}`
	}
	return toolErrorPrefix + string(payload)
}
