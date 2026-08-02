package subagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
)

// buildToolSet creates the tool definitions available to the subagent,
// applying allowed/forbidden filters from the definition.
func (d *SubagentDelegate) buildToolSet(def SubagentDefinition, toolExec *agenttools.ToolExecutor) []gateway.ToolDefinition {
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
	if d.depth+1 < d.delegationDepthLimit() && toolExplicitlyAllowed(agenttools.ToolNameDelegate, def) {
		allTools = append(allTools, DelegateToolDefinition())
	}
	if d.depth+1 < d.delegationDepthLimit() && toolExplicitlyAllowed(agenttools.ToolNameDelegateParallel, def) {
		allTools = append(allTools, DelegateParallelToolDefinition())
	}

	if len(def.AllowedTools) == 0 && len(def.ForbiddenTools) == 0 {
		gateway.SortTools(allTools)
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
	gateway.SortTools(filtered)
	return filtered
}

func (d *SubagentDelegate) capabilityToolDefinitions(ctx context.Context) []gateway.ToolDefinition {
	var tools []gateway.ToolDefinition
	seen := make(map[string]bool)
	appendTools := func(registry *capabilities.Registry) {
		if registry == nil {
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
	appendTools(d.scopedCapabilities)
	appendTools(d.capabilities)
	gateway.SortTools(tools)
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

	b.WriteString("\n\n")
	b.WriteString(agenthooks.ExternalContentInstruction)
	b.WriteString("\n\nWhen done, provide your final answer as plain text without calling any further tools.")
	return b.String()
}

// executeTool handles a single tool call within the subagent, enforcing restrictions.
func (d *SubagentDelegate) executeTool(
	ctx context.Context,
	call gateway.ToolCall,
	def SubagentDefinition,
	toolExec *agenttools.ToolExecutor,
) string {
	if isToolForbidden(call.Function.Name, def) {
		return agenttools.JSONErrorf("tool %q is not available to this subagent", call.Function.Name)
	}
	switch call.Function.Name {
	case agenttools.ToolNameBash, agenttools.ToolNameRead, agenttools.ToolNameWrite:
		return toolExec.Execute(ctx, call)
	case agenttools.ToolNameDelegate:
		return d.runDelegate(ctx, call)
	case agenttools.ToolNameDelegateParallel:
		return d.runDelegateParallel(ctx, call)
	default:
		raw := agenttools.ExecuteCapabilityTool(ctx, call, nil, d.capabilities, d.scopedCapabilities, d.callEnv)
		return agenthooks.ApplyInjectionResistance(call.Function.Name, raw, d.externalTools, agenttools.ToolStatusSuccess, true)
	}
}

// ExecuteTool handles one subagent tool call while enforcing definition-level
// restrictions.
func (d *SubagentDelegate) ExecuteTool(
	ctx context.Context,
	call gateway.ToolCall,
	def SubagentDefinition,
	toolExec *agenttools.ToolExecutor,
) string {
	return d.executeTool(ctx, call, def, toolExec)
}

func (d *SubagentDelegate) runDelegate(ctx context.Context, call gateway.ToolCall) string {
	if d.depth+1 >= d.delegationDepthLimit() {
		return agenttools.JSONErrorf("%s", ErrDepthExceeded.Error())
	}
	var args delegateArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return agenttools.JSONErrorf("invalid delegate arguments: %v", err)
	}
	if args.Subagent == "" {
		return agenttools.JSONErrorf("subagent name is required")
	}
	if args.Task == "" {
		return agenttools.JSONErrorf("task description is required")
	}
	loader := &SubagentLoader{}
	subDef, err := loader.LoadByName(d.workspacePath, args.Subagent)
	if err != nil {
		return agenttools.JSONErrorf("failed to load subagent definition: %v", err)
	}
	child := NewSubagentDelegate(
		d.gateway,
		d.sandbox,
		d.workspacePath,
		d.envVars,
		d.wallTimeout,
		d.depth+1,
	).withMaxDelegationDepth(d.delegationDepthLimit()).
		WithCapabilities(d.capabilities, d.scopedCapabilities).
		WithExternalTools(d.externalTools).
		WithCallEnv(d.callEnv)
	result, err := child.Delegate(ctx, *subDef, args.Task, "", "", 0.2, 0)
	if err != nil {
		return agenttools.JSONErrorf("delegation failed: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return agenttools.JSONErrorf("failed to encode subagent result: %v", err)
	}
	return string(encoded)
}

func (d *SubagentDelegate) runDelegateParallel(ctx context.Context, call gateway.ToolCall) string {
	if d.depth+1 >= d.delegationDepthLimit() {
		return agenttools.JSONErrorf("%s", ErrDepthExceeded.Error())
	}
	var args delegateParallelArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return agenttools.JSONErrorf("invalid delegate_parallel arguments: %v", err)
	}
	if len(args.Tasks) == 0 {
		return agenttools.JSONErrorf("delegate_parallel requires at least one task")
	}
	loader := &SubagentLoader{}
	tasks := make([]ParallelTask, 0, len(args.Tasks))
	for i, task := range args.Tasks {
		if task.Subagent == "" {
			return agenttools.JSONErrorf("task %d subagent name is required", i)
		}
		if task.Task == "" {
			return agenttools.JSONErrorf("task %d description is required", i)
		}
		subDef, err := loader.LoadByName(d.workspacePath, task.Subagent)
		if err != nil {
			return agenttools.JSONErrorf("failed to load subagent definition for task %d: %v", i, err)
		}
		tasks = append(tasks, ParallelTask{
			Definition:  *subDef,
			Description: task.Task,
		})
	}
	child := NewSubagentDelegate(
		d.gateway,
		d.sandbox,
		d.workspacePath,
		d.envVars,
		d.wallTimeout,
		d.depth+1,
	).withMaxDelegationDepth(d.delegationDepthLimit()).
		WithCapabilities(d.capabilities, d.scopedCapabilities).
		WithExternalTools(d.externalTools).
		WithCallEnv(d.callEnv)
	results := child.DelegateParallel(ctx, tasks, "", "", 0.2, 0)
	encoded, err := json.Marshal(results)
	if err != nil {
		return agenttools.JSONErrorf("failed to encode subagent results: %v", err)
	}
	return string(encoded)
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
