package testutil

import (
	"encoding/json"
	"strings"

	"agentd/internal/gateway/spec"
)

// BashToolCall returns the tool call an agentic model makes to run command
// through the bash tool. Tests script it where a legacy reply was {"command": ...}.
func BashToolCall(id, command string) spec.ToolCall {
	args, _ := json.Marshal(map[string]string{"command": command})
	return spec.ToolCall{
		ID:   id,
		Type: "function",
		Function: spec.ToolCallFunction{
			Name:      "bash",
			Arguments: string(args),
		},
	}
}

// ReadToolCall returns the tool call an agentic model makes to read path
// through the read tool.
func ReadToolCall(id, path string) spec.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return spec.ToolCall{
		ID:       id,
		Type:     "function",
		Function: spec.ToolCallFunction{Name: "read", Arguments: string(args)},
	}
}

// ProviderSupportsChatTools is the answer a test gateway gives for
// ProviderSupportsChatTools: any named provider can call tools. Fakes that
// exercise the agentic loop delegate to it so their fixtures are not dropped
// to the legacy path.
func ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}
