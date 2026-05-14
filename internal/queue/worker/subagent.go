package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// MaxDelegationDepth is the harness-enforced limit on delegation nesting.
// Depth=1 means a parent can delegate but a subagent cannot.
const MaxDelegationDepth = 1

// SubagentStatus represents the terminal state of a subagent execution.
type SubagentStatus string

const (
	SubagentStatusSuccess SubagentStatus = "success"
	SubagentStatusFailure SubagentStatus = "failure"
	SubagentStatusTimeout SubagentStatus = "timeout"
)

// SubagentDefinition describes a subagent's purpose, tool constraints,
// context budget, output schema, and termination criteria.
// Definitions are loaded from markdown files in <workspace>/.agentd/subagents/.
type SubagentDefinition struct {
	// Name uniquely identifies this subagent type.
	Name string `json:"name"`
	// Purpose describes what this subagent does (injected as system prompt).
	Purpose string `json:"purpose"`
	// AllowedTools lists tools the subagent may use. Empty means all built-in tools.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// ForbiddenTools lists tools explicitly denied to the subagent.
	ForbiddenTools []string `json:"forbidden_tools,omitempty"`
	// MaxIterations caps the subagent's tool loop. Zero uses a default of 20.
	MaxIterations int `json:"max_iterations,omitempty"`
	// OutputSchema describes the expected structure of the subagent's output.
	OutputSchema string `json:"output_schema,omitempty"`
	// TerminationCriteria describes when the subagent should stop.
	TerminationCriteria string `json:"termination_criteria,omitempty"`
	// SourcePath records the file this definition was loaded from.
	SourcePath string `json:"-"`
}

// SubagentResult is the structured output returned from a subagent execution.
type SubagentResult struct {
	// Status indicates whether the subagent completed successfully.
	Status SubagentStatus `json:"status"`
	// Output is the final text produced by the subagent.
	Output string `json:"output"`
	// FilesModified lists workspace-relative paths the subagent wrote to.
	FilesModified []string `json:"files_modified,omitempty"`
	// ToolsCalled lists the tool names invoked during execution.
	ToolsCalled []string `json:"tools_called,omitempty"`
	// Error carries any error message when status != success.
	Error string `json:"error,omitempty"`
	// Iterations is the number of tool-calling rounds executed.
	Iterations int `json:"iterations"`
}

// SubagentDelegate creates and runs an isolated harness for a subagent.
type SubagentDelegate struct {
	gateway       gateway.AIGateway
	sandbox       sandbox.Executor
	workspacePath string
	envVars       []string
	wallTimeout   time.Duration
	depth         int
}

// NewSubagentDelegate constructs a delegate at the given depth.
func NewSubagentDelegate(
	gw gateway.AIGateway,
	sb sandbox.Executor,
	workspacePath string,
	envVars []string,
	wallTimeout time.Duration,
	depth int,
) *SubagentDelegate {
	return &SubagentDelegate{
		gateway:       gw,
		sandbox:       sb,
		workspacePath: workspacePath,
		envVars:       envVars,
		wallTimeout:   wallTimeout,
		depth:         depth,
	}
}

// ErrDepthExceeded is returned when a delegation attempt exceeds MaxDelegationDepth.
var ErrDepthExceeded = errors.New("subagent delegation depth exceeded")

// Delegate runs a subagent with isolated context and restricted tools.
// The subagent's internal reasoning never enters the parent's context.
func (d *SubagentDelegate) Delegate(
	ctx context.Context,
	def SubagentDefinition,
	taskDescription string,
	provider, model string,
	temperature float64,
	maxTokens int,
) (*SubagentResult, error) {
	if d.depth >= MaxDelegationDepth {
		return nil, ErrDepthExceeded
	}

	maxIter := def.MaxIterations
	if maxIter <= 0 {
		maxIter = 20
	}

	toolExec := NewToolExecutor(d.sandbox, d.workspacePath, d.envVars, d.wallTimeout)
	tools := d.buildToolSet(def, toolExec)

	systemPrompt := d.buildSystemPrompt(def)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: taskDescription},
	}

	result := &SubagentResult{
		Status: SubagentStatusSuccess,
	}
	toolsCalled := make(map[string]struct{})
	var filesModified []string

	for i := 0; i < maxIter; i++ {
		req := gateway.AIRequest{
			Messages:    messages,
			Temperature: temperature,
			Tools:       tools,
			Provider:    provider,
			Model:       model,
			MaxTokens:   maxTokens,
			Role:        gateway.RoleWorker,
		}

		resp, err := d.gateway.Generate(ctx, req)
		if err != nil {
			result.Status = SubagentStatusFailure
			result.Error = fmt.Sprintf("gateway error: %v", err)
			result.Iterations = i + 1
			return result, nil
		}

		messages = append(messages, gateway.PromptMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
		})

		if len(resp.ToolCalls) == 0 {
			result.Output = resp.Content
			result.Iterations = i + 1
			break
		}

		for _, call := range resp.ToolCalls {
			toolsCalled[call.Function.Name] = struct{}{}
			callResult := d.executeTool(ctx, call, def, toolExec)

			if call.Function.Name == toolNameWrite {
				if path := extractWritePath(call.Function.Arguments); path != "" {
					filesModified = append(filesModified, path)
				}
			}

			messages = append(messages, gateway.PromptMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    callResult,
			})
		}

		if i == maxIter-1 {
			result.Status = SubagentStatusTimeout
			result.Error = "max iterations reached"
			result.Output = messages[len(messages)-1].Content
		}

		result.Iterations = i + 1
	}

	for tool := range toolsCalled {
		result.ToolsCalled = append(result.ToolsCalled, tool)
	}
	result.FilesModified = filesModified
	return result, nil
}

// DelegateParallel runs multiple subagent tasks concurrently and collects results.
func (d *SubagentDelegate) DelegateParallel(
	ctx context.Context,
	tasks []ParallelTask,
	provider, model string,
	temperature float64,
	maxTokens int,
) []*SubagentResult {
	results := make([]*SubagentResult, len(tasks))
	done := make(chan struct{}, len(tasks))

	for i, task := range tasks {
		go func(idx int, t ParallelTask) {
			defer func() { done <- struct{}{} }()
			res, err := d.Delegate(ctx, t.Definition, t.Description, provider, model, temperature, maxTokens)
			if err != nil {
				results[idx] = &SubagentResult{
					Status: SubagentStatusFailure,
					Error:  err.Error(),
				}
			} else {
				results[idx] = res
			}
		}(i, task)
	}

	for range tasks {
		<-done
	}
	return results
}

// ParallelTask bundles a definition with a task description for parallel delegation.
type ParallelTask struct {
	Definition  SubagentDefinition
	Description string
}

// buildToolSet creates the tool definitions available to the subagent,
// applying allowed/forbidden filters from the definition.
func (d *SubagentDelegate) buildToolSet(def SubagentDefinition, toolExec *ToolExecutor) []gateway.ToolDefinition {
	allTools := toolExec.Definitions()

	if len(def.AllowedTools) == 0 && len(def.ForbiddenTools) == 0 {
		return allTools
	}

	allowed := make(map[string]bool)
	for _, t := range def.AllowedTools {
		allowed[t] = true
	}
	forbidden := make(map[string]bool)
	for _, t := range def.ForbiddenTools {
		forbidden[t] = true
	}

	var filtered []gateway.ToolDefinition
	for _, tool := range allTools {
		if forbidden[tool.Name] {
			continue
		}
		if len(def.AllowedTools) > 0 && !allowed[tool.Name] {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
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
	return toolExec.Execute(ctx, call)
}

// isToolForbidden returns true if the tool is explicitly forbidden or not in the allowed list.
func isToolForbidden(name string, def SubagentDefinition) bool {
	for _, t := range def.ForbiddenTools {
		if t == name {
			return true
		}
	}
	if len(def.AllowedTools) > 0 {
		for _, t := range def.AllowedTools {
			if t == name {
				return false
			}
		}
		return true
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

// DelegateToolDefinition returns the tool definition for the delegate tool
// that the parent agent uses to trigger subagent execution.
func DelegateToolDefinition() gateway.ToolDefinition {
	return gateway.ToolDefinition{
		Name:        toolNameDelegate,
		Description: "Delegate a bounded sub-task to a specialized subagent. The subagent runs in isolation with its own context and restricted tool set. Returns a structured result.",
		Parameters: &gateway.FunctionParameters{
			Type: "object",
			Properties: map[string]any{
				"subagent": map[string]any{
					"type":        "string",
					"description": "Name of the subagent definition to use (from .agentd/subagents/)",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "Description of the sub-task to delegate",
				},
			},
			Required: []string{"subagent", "task"},
		},
	}
}

// delegateArgs holds the parsed arguments for the delegate tool call.
type delegateArgs struct {
	Subagent string `json:"subagent"`
	Task     string `json:"task"`
}

// executeDelegate handles a delegate tool call from the parent agent.
func (w *Worker) executeDelegate(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor) string {
	var args delegateArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return jsonErrorf("invalid delegate arguments: %v", err)
	}
	if args.Subagent == "" {
		return jsonErrorf("subagent name is required")
	}
	if args.Task == "" {
		return jsonErrorf("task description is required")
	}

	loader := &SubagentLoader{}
	def, err := loader.LoadByName(toolExecutor.workspacePath, args.Subagent)
	if err != nil {
		return jsonErrorf("failed to load subagent definition: %v", err)
	}

	delegate := NewSubagentDelegate(
		w.gateway,
		w.sandbox,
		toolExecutor.workspacePath,
		toolExecutor.envVars,
		toolExecutor.wallTimeout,
		0, // depth=0: parent is delegating
	)

	result, err := delegate.Delegate(
		ctx,
		*def,
		args.Task,
		"", // use default provider
		"", // use default model
		0.2,
		0,
	)
	if err != nil {
		return jsonErrorf("delegation failed: %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return jsonErrorf("failed to encode subagent result: %v", err)
	}
	return string(encoded)
}
