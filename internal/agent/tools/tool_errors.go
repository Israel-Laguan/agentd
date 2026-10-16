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

func JSONErrorf(format string, args ...any) string {
	payload, err := json.Marshal(map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
	if err != nil {
		return toolErrorPrefix + `{"error":"failed to encode error payload"}`
	}
	return toolErrorPrefix + string(payload)
}
