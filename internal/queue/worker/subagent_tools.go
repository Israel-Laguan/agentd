package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"agentd/internal/gateway"
)

// buildToolSet creates the tool definitions available to the subagent,
// applying allowed/forbidden filters from the definition.
func (d *SubagentDelegate) buildToolSet(def SubagentDefinition, toolExec *ToolExecutor) []gateway.ToolDefinition {
	normalizeSet := func(values []string) map[string]bool {
		out := make(map[string]bool, len(values))
		for _, v := range values {
			key := strings.ToLower(strings.TrimSpace(v))
			if key != "" {
				out[key] = true
			}
		}
		return out
	}

	allTools := append([]gateway.ToolDefinition(nil), toolExec.Definitions()...)
	allTools = append(allTools, d.capabilityToolDefinitions(context.Background())...)
	if toolExplicitlyAllowed(toolNameDelegate, def) {
		allTools = append(allTools, DelegateToolDefinition())
	}
	if toolExplicitlyAllowed(toolNameDelegateParallel, def) {
		allTools = append(allTools, DelegateParallelToolDefinition())
	}

	if len(def.AllowedTools) == 0 && len(def.ForbiddenTools) == 0 {
		return allTools
	}

	allowed := normalizeSet(def.AllowedTools)
	forbidden := normalizeSet(def.ForbiddenTools)

	var filtered []gateway.ToolDefinition
	for _, tool := range allTools {
		name := strings.ToLower(tool.Name)
		if forbidden[name] {
			continue
		}
		if len(def.AllowedTools) > 0 && !allowed[name] {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func (d *SubagentDelegate) capabilityToolDefinitions(ctx context.Context) []gateway.ToolDefinition {
	var tools []gateway.ToolDefinition
	seen := make(map[string]bool)
	appendTools := func(registry interface {
		GetToolsAndAdapterIndex(context.Context) ([]gateway.ToolDefinition, map[string]string, error)
	}, isNil bool) {
		if isNil {
			return
		}
		registryTools, _, err := registry.GetToolsAndAdapterIndex(ctx)
		if err != nil {
			slog.Warn("failed to get subagent capability tools", "error", err)
			return
		}
		for _, tool := range registryTools {
			key := strings.ToLower(strings.TrimSpace(tool.Name))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			tools = append(tools, tool)
		}
	}
	appendTools(d.scopedCapabilities, d.scopedCapabilities == nil)
	appendTools(d.capabilities, d.capabilities == nil)
	return tools
}

// buildSystemPrompt constructs the subagent's system prompt from its definition.
func (d *SubagentDelegate) buildSystemPrompt(def SubagentDefinition) string {
	var b strings.Builder
	b.WriteString("You are a specialized subagent.\n\n")
	b.WriteString("## Purpose\n")
	b.WriteString(def.Purpose)
	b.WriteString("\n")

	if def.OutputSchema != "" {
		b.WriteString("\n## Output Schema\n")
		b.WriteString(def.OutputSchema)
		b.WriteString("\n")
	}

	if def.TerminationCriteria != "" {
		b.WriteString("\n## Termination Criteria\n")
		b.WriteString(def.TerminationCriteria)
		b.WriteString("\n")
	}

	b.WriteString("\nWhen done, provide your final answer as plain text without calling any further tools.")
	return b.String()
}

// executeTool handles a single tool call within the subagent, enforcing restrictions.
func (d *SubagentDelegate) executeTool(
	ctx context.Context,
	call gateway.ToolCall,
	def SubagentDefinition,
	toolExec *ToolExecutor,
) string {
	if isToolForbidden(call.Function.Name, def) {
		return jsonErrorf("tool %q is not available to this subagent", call.Function.Name)
	}
	switch call.Function.Name {
	case toolNameBash, toolNameRead, toolNameWrite:
		return toolExec.Execute(ctx, call)
	case toolNameDelegate, toolNameDelegateParallel:
		return jsonErrorf("%s", ErrDepthExceeded.Error())
	default:
		return executeCapabilityTool(ctx, call, nil, d.capabilities, d.scopedCapabilities)
	}
}

// isToolForbidden returns true if the tool is explicitly forbidden or not in the allowed list.
func isToolForbidden(name string, def SubagentDefinition) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range def.ForbiddenTools {
		if strings.ToLower(strings.TrimSpace(t)) == name {
			return true
		}
	}
	if len(def.AllowedTools) > 0 {
		for _, t := range def.AllowedTools {
			if strings.ToLower(strings.TrimSpace(t)) == name {
				return false
			}
		}
		return true
	}
	return false
}

func toolExplicitlyAllowed(name string, def SubagentDefinition) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, allowed := range def.AllowedTools {
		if strings.ToLower(strings.TrimSpace(allowed)) == name {
			return true
		}
	}
	return false
}

// extractWritePath extracts the path argument from a write tool call's JSON arguments.
func extractWritePath(argsJSON string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		slog.Debug("failed to extract write path from subagent tool call", "error", err)
		return ""
	}
	return args.Path
}
