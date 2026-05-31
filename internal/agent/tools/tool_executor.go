package tools

import (
	"context"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"

	wfilecontext "agentd/internal/agent/filecontext"
)

const (
	toolNameBash             = "bash"
	toolNameRead             = "read"
	toolNameWrite            = "write"
	toolNameDelegate         = "delegate"
	toolNameDelegateParallel = "delegate_parallel"

	defaultMaxToolReadFileBytes = 10 << 20 // 10 MiB

	// toolErrorPrefix marks strings from jsonErrorf and sandboxFailureJSON so classifiers can tell
	// tool failures apart from file contents or command stdout that happen to
	// be single-key {"error":"..."} JSON.
	toolErrorPrefix = "\x1eagentd/tool-error\x1e"
)

const (
	ToolNameBash             = toolNameBash
	ToolNameRead             = toolNameRead
	ToolNameWrite            = toolNameWrite
	ToolNameDelegate         = toolNameDelegate
	ToolNameDelegateParallel = toolNameDelegateParallel
	ToolErrorPrefix          = toolErrorPrefix
)

type ToolExecutor struct {
	sandbox       sandbox.Executor
	workspacePath string
	envVars       []string
	wallTimeout   time.Duration
	maxReadBytes  int64
	filePipeline  *wfilecontext.FilePipeline

	workspaceRoot     string
	workspaceRootErr  error
	workspaceRootOnce sync.Once
}

func NewToolExecutor(sb sandbox.Executor, workspacePath string, envVars []string, wallTimeout time.Duration) *ToolExecutor {
	return &ToolExecutor{
		sandbox:       sb,
		workspacePath: workspacePath,
		envVars:       envVars,
		wallTimeout:   wallTimeout,
		maxReadBytes:  defaultMaxToolReadFileBytes,
	}
}

func (t *ToolExecutor) Execute(ctx context.Context, call gateway.ToolCall, extraEnv ...string) string {
	switch call.Function.Name {
	case toolNameBash:
		return t.executeBash(ctx, call.Function.Arguments, extraEnv...)
	case toolNameRead:
		return t.executeRead(ctx, call.Function.Arguments)
	case toolNameWrite:
		return t.executeWrite(ctx, call.Function.Arguments)
	default:
		return jsonErrorf("unknown tool: %s", call.Function.Name)
	}
}

// SchemaRegistryFromDefinitions builds a tool-name → parameters map for SchemaValidationHook.
func SchemaRegistryFromDefinitions(defs []gateway.ToolDefinition) map[string]*gateway.FunctionParameters {
	registry := make(map[string]*gateway.FunctionParameters, len(defs))
	for _, def := range defs {
		if def.Parameters != nil {
			registry[def.Name] = def.Parameters
		}
	}
	return registry
}

func (t *ToolExecutor) Definitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{
			Name:        toolNameBash,
			Description: "Execute a shell command in the sandbox. Returns stdout/stderr output.",
			Parameters: &gateway.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"command": map[string]any{"type": "string", "description": "The shell command to execute"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        toolNameRead,
			Description: "Read a file from the workspace. Returns file contents or error.",
			Cacheable:   true,
			Parameters: &gateway.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"path": map[string]any{"type": "string", "description": "Relative path to the file from workspace root"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        toolNameWrite,
			Description: "Write content to a file in the workspace. Creates or overwrites.",
			Parameters: &gateway.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"path":    map[string]any{"type": "string", "description": "Relative path to the file from workspace root"},
					"content": map[string]any{"type": "string", "description": "Content to write to the file"},
				},
				Required: []string{"path", "content"},
			},
		},
	}
}



// BuildEnv returns a copy of the executor base environment merged with extra
// KEY=VALUE pairs for a single tool call without mutating executor state.
func (t *ToolExecutor) BuildEnv(extra ...string) []string {
	out := make([]string, 0, len(t.envVars)+len(extra))
	out = append(out, t.envVars...)
	out = append(out, extra...)
	return out
}

func (t *ToolExecutor) EnvVars() []string {
	if t == nil {
		return nil
	}
	return append([]string(nil), t.envVars...)
}

func (t *ToolExecutor) WorkspacePath() string {
	if t == nil {
		return ""
	}
	return t.workspacePath
}

func (t *ToolExecutor) WallTimeout() time.Duration {
	if t == nil {
		return 0
	}
	return t.wallTimeout
}

func (t *ToolExecutor) SetFilePipeline(filePipeline *wfilecontext.FilePipeline) {
	if t == nil {
		return
	}
	t.filePipeline = filePipeline
}

func (t *ToolExecutor) SetMaxReadBytes(maxReadBytes int64) {
	if t == nil {
		return
	}
	t.maxReadBytes = maxReadBytes
}



