package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

type bashArgs struct {
	Command string `json:"command"`
}

// BuildEnv returns a copy of the executor base environment merged with extra
// KEY=VALUE pairs for a single tool call without mutating executor state.
func (t *ToolExecutor) BuildEnv(extra ...string) []string {
	out := make([]string, 0, len(t.envVars)+len(extra))
	out = append(out, t.envVars...)
	out = append(out, extra...)
	return out
}

func (t *ToolExecutor) executeBash(ctx context.Context, argsJSON string, extraEnv ...string) string {
	var args bashArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Command == "" {
		return jsonErrorf("command is required")
	}

	payload := sandbox.Payload{
		TaskID:        "tool",
		ProjectID:     "",
		WorkspacePath: t.workspacePath,
		Command:       args.Command,
		EnvVars:       t.BuildEnv(extraEnv...),
		WallTimeout:   t.wallTimeout,
	}

	result, err := t.sandbox.Execute(ctx, payload)
	if err != nil {
		return jsonErrorf("execution failed: %v", err)
	}

	if !result.Success {
		return sandboxFailureJSON(result)
	}

	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.Stderr
	}
	return output
}

type readArgs struct {
	Path string `json:"path"`
}

func (t *ToolExecutor) executeRead(ctx context.Context, argsJSON string) string {
	if err := ctx.Err(); err != nil {
		return jsonErrorf("read cancelled: %v", err)
	}

	var args readArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Path == "" {
		return jsonErrorf("path is required")
	}

	fullPath, err := t.resolvePath(args.Path, false)
	if err != nil {
		return jsonErrorf("%v", err)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return jsonErrorf("stat failed: %v", err)
	}
	if info.Size() > t.maxReadBytes {
		return jsonErrorf("file too large: %d bytes (max %d)", info.Size(), t.maxReadBytes)
	}

	content, err := readFileWithContext(ctx, fullPath, t.maxReadBytes, info)
	if err != nil {
		if ctx.Err() != nil {
			return jsonErrorf("read cancelled: %v", ctx.Err())
		}
		return jsonErrorf("read failed: %v", err)
	}

	if t.filePipeline != nil {
		markdown, pipeErr := t.filePipeline.ProcessRead(ctx, args.Path, fullPath, content, info)
		if pipeErr != nil {
			if ctx.Err() != nil || errors.Is(pipeErr, context.Canceled) || errors.Is(pipeErr, context.DeadlineExceeded) {
				return jsonErrorf("read cancelled: %v", pipeErr)
			}
			slog.Warn("file pipeline failed, returning raw content", "path", args.Path, "error", pipeErr)
			return string(content)
		}
		return markdown
	}

	return string(content)
}

type writeArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

func (t *ToolExecutor) executeWrite(ctx context.Context, argsJSON string) string {
	if err := ctx.Err(); err != nil {
		return jsonErrorf("write cancelled: %v", err)
	}

	var args writeArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Path == "" {
		return jsonErrorf("path is required")
	}
	if args.Content == nil {
		return jsonErrorf("content is required")
	}

	fullPath, err := t.resolvePath(args.Path, true)
	if err != nil {
		return jsonErrorf("%v", err)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return jsonErrorf("create directory failed: %v", err)
	}

	if err := os.WriteFile(fullPath, []byte(*args.Content), 0644); err != nil {
		return jsonErrorf("write failed: %v", err)
	}

	return `{"success": true}`
}

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

func jsonErrorf(format string, args ...any) string {
	payload, err := json.Marshal(map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
	if err != nil {
		return toolErrorPrefix + `{"error":"failed to encode error payload"}`
	}
	return toolErrorPrefix + string(payload)
}

func isToolErrorPayload(raw string) bool {
	return strings.HasPrefix(raw, toolErrorPrefix)
}

func stripToolErrorPrefix(raw string) string {
	return strings.TrimPrefix(raw, toolErrorPrefix)
}
