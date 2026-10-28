package worker

import (
	"context"
	"encoding/json"
	"strings"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/toolenv"
)

// executeCapabilityTool routes a capability (MCP) tool call to the correct registry.
//
// It uses resolveCapabilityRoute to check the scoped registry first, then falls back
// to the global registry. This means that if both registries provide the same tool,
// the scoped version wins — the intended behavior for project-scoped plugins.
//
// Historical context: In earlier iterations of processAgenticIteration, the taskCaps
// parameter was declared as _ *capabilities.Registry (unused). Scoped capability
// tools were advertised to the LLM via agenticToolsWithExtras, but they could not
// actually be executed because dispatchToolWithProject always called w.capabilities.CallTool
// directly, bypassing any scoped registry. Now the scoped registry is wired through
// handleAgenticToolCalls → dispatchToolWithHooks → dispatchToolWithProject, so
// scoped tools are both advertised and executable.
func executeCapabilityTool(ctx context.Context, call gateway.ToolCall, toolToAdapter map[string]string, global, scoped *capabilities.Registry, callEnv []string) string {
	args, err := parseCapabilityArgs(call.Function.Arguments)
	if err != nil {
		return agenttools.JSONErrorf("invalid arguments: %v", err)
	}
	adapterName := ""
	if toolToAdapter != nil {
		adapterName = toolToAdapter[call.Function.Name]
	}

	registry, adapterName := resolveCapabilityRoute(ctx, call.Function.Name, adapterName, global, scoped)
	if registry == nil {
		return agenttools.JSONErrorf("unknown tool: %s", call.Function.Name)
	}
	toolCtx := toolenv.With(ctx, callEnv)
	out, err := registry.CallTool(toolCtx, adapterName, call.Function.Name, args)
	if err != nil {
		return agenttools.JSONErrorf("capability tool failed: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return agenttools.JSONErrorf("capability tool result encode failed: %v", err)
	}
	return string(encoded)
}

func parseCapabilityArgs(argsJSON string) (map[string]any, error) {
	if strings.TrimSpace(argsJSON) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// resolveCapabilityRoute decides which capability registry should handle a tool call.
// The scoped registry is checked first so that project-scoped plugins take priority
// over global ones when both provide the same tool. This was not wired in earlier
// iterations (taskCaps was unused), so scoped tools were advertised to the LLM but
// could not actually be executed.
func resolveCapabilityRoute(ctx context.Context, toolName, adapterHint string, global, scoped *capabilities.Registry) (*capabilities.Registry, string) {
	if registry, adapterName := resolveCapabilityInRegistry(ctx, toolName, adapterHint, scoped); registry != nil {
		return registry, adapterName
	}
	return resolveCapabilityInRegistry(ctx, toolName, adapterHint, global)
}

func resolveCapabilityInRegistry(ctx context.Context, toolName, adapterHint string, registry *capabilities.Registry) (*capabilities.Registry, string) {
	if registry == nil {
		return nil, ""
	}
	if adapterHint != "" {
		if _, ok := registry.GetAdapter(adapterHint); ok {
			return registry, adapterHint
		}
	}
	if adapterName, ok := registry.AdapterForTool(ctx, toolName); ok {
		return registry, adapterName
	}
	return nil, ""
}
