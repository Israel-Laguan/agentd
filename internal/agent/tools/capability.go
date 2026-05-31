package tools

import (
	"context"
	"encoding/json"
	"strings"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/toolenv"
)

// ExecuteCapabilityTool routes a capability tool call to the scoped registry
// first, then the global registry.
func ExecuteCapabilityTool(ctx context.Context, call gateway.ToolCall, toolToAdapter map[string]string, global, scoped *capabilities.Registry, callEnv []string) string {
	args, err := parseCapabilityArgs(call.Function.Arguments)
	if err != nil {
		return JSONErrorf("invalid arguments: %v", err)
	}
	adapterName := ""
	if toolToAdapter != nil {
		adapterName = toolToAdapter[call.Function.Name]
	}

	registry, adapterName := ResolveCapabilityRoute(ctx, call.Function.Name, adapterName, global, scoped)
	if registry == nil {
		return JSONErrorf("unknown tool: %s", call.Function.Name)
	}
	toolCtx := toolenv.With(ctx, callEnv)
	out, err := registry.CallTool(toolCtx, adapterName, call.Function.Name, args)
	if err != nil {
		return JSONErrorf("capability tool failed: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return JSONErrorf("capability tool result encode failed: %v", err)
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

// ResolveCapabilityRoute decides which capability registry should handle a tool
// call. The scoped registry is checked first so project-scoped tools win.
func ResolveCapabilityRoute(ctx context.Context, toolName, adapterHint string, global, scoped *capabilities.Registry) (*capabilities.Registry, string) {
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
