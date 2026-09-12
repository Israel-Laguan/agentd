package tools

import (
	"encoding/json"
	"fmt"

	"agentd/internal/sandbox"
)

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
