package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/sandbox"
)

func sandboxFailureJSON(result sandbox.Result) string {
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

func SandboxFailureJSON(result sandbox.Result) string {
	return sandboxFailureJSON(result)
}

func jsonErrorf(format string, args ...any) string {
	payload, err := json.Marshal(map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
	if err != nil {
		return toolErrorPrefix + `{"error":"failed to encode error payload"}`
	}
	return toolErrorPrefix + string(payload)
}

func JSONErrorf(format string, args ...any) string {
	return jsonErrorf(format, args...)
}

func isToolErrorPayload(raw string) bool {
	return strings.HasPrefix(raw, toolErrorPrefix)
}

func IsToolErrorPayload(raw string) bool {
	return isToolErrorPayload(raw)
}

func stripToolErrorPrefix(raw string) string {
	return strings.TrimPrefix(raw, toolErrorPrefix)
}

func StripToolErrorPrefix(raw string) string {
	return stripToolErrorPrefix(raw)
}
