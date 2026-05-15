package worker

import (
	"errors"
	"time"

	"agentd/internal/capabilities"
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
	// ContextBudget caps the subagent prompt context by character count. Zero means unlimited.
	ContextBudget int `json:"context_budget,omitempty"`
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
	gateway            gateway.AIGateway
	sandbox            sandbox.Executor
	workspacePath      string
	envVars            []string
	wallTimeout        time.Duration
	depth              int
	maxDelegationDepth int
	capabilities       *capabilities.Registry
	scopedCapabilities *capabilities.Registry
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

func (d *SubagentDelegate) delegationDepthLimit() int {
	if d.maxDelegationDepth > 0 {
		return d.maxDelegationDepth
	}
	return MaxDelegationDepth
}

func (d *SubagentDelegate) withMaxDelegationDepth(maxDepth int) *SubagentDelegate {
	d.maxDelegationDepth = maxDepth
	return d
}

// WithCapabilities configures the global and scoped capability registries used
// by the delegate when exposing and executing MCP/capability tools.
func (d *SubagentDelegate) WithCapabilities(global, scoped *capabilities.Registry) *SubagentDelegate {
	d.capabilities = global
	d.scopedCapabilities = scoped
	return d
}

// ErrDepthExceeded is returned when a delegation attempt exceeds MaxDelegationDepth.
var ErrDepthExceeded = errors.New("subagent delegation depth exceeded")

// ParallelTask bundles a definition with a task description for parallel delegation.
type ParallelTask struct {
	Definition  SubagentDefinition
	Description string
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

// DelegateParallelToolDefinition returns the tool definition for running
// independent subagent tasks concurrently.
func DelegateParallelToolDefinition() gateway.ToolDefinition {
	return gateway.ToolDefinition{
		Name:        toolNameDelegateParallel,
		Description: "Delegate independent bounded sub-tasks to specialized subagents concurrently. Returns structured results in input order.",
		Parameters: &gateway.FunctionParameters{
			Type: "object",
			Properties: map[string]any{
				"tasks": map[string]any{
					"type":        "array",
					"description": "Subagent tasks to run concurrently",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"subagent": map[string]any{
								"type":        "string",
								"description": "Name of the subagent definition to use",
							},
							"task": map[string]any{
								"type":        "string",
								"description": "Description of the sub-task to delegate",
							},
						},
						"required": []string{"subagent", "task"},
					},
				},
			},
			Required: []string{"tasks"},
		},
	}
}

// delegateArgs holds the parsed arguments for the delegate tool call.
type delegateArgs struct {
	Subagent string `json:"subagent"`
	Task     string `json:"task"`
}

type delegateParallelArgs struct {
	Tasks []delegateArgs `json:"tasks"`
}
