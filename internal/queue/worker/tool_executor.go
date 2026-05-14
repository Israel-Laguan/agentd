package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

const (
	toolNameBash     = "bash"
	toolNameRead     = "read"
	toolNameWrite    = "write"
	toolNameDelegate = "delegate"

	defaultMaxToolReadFileBytes = 10 << 20 // 10 MiB
)

// maxToolReadFileBytes caps read tool file size to avoid loading huge files into memory (tests may override).
var maxToolReadFileBytes = int64(defaultMaxToolReadFileBytes)

type ToolExecutor struct {
	sandbox       sandbox.Executor
	workspacePath string
	envVars       []string
	wallTimeout   time.Duration
}

func NewToolExecutor(sb sandbox.Executor, workspacePath string, envVars []string, wallTimeout time.Duration) *ToolExecutor {
	return &ToolExecutor{
		sandbox:       sb,
		workspacePath: workspacePath,
		envVars:       envVars,
		wallTimeout:   wallTimeout,
	}
}

func (t *ToolExecutor) Execute(ctx context.Context, call gateway.ToolCall) string {
	switch call.Function.Name {
	case toolNameBash:
		return t.executeBash(ctx, call.Function.Arguments)
	case toolNameRead:
		return t.executeRead(call.Function.Arguments)
	case toolNameWrite:
		return t.executeWrite(call.Function.Arguments)
	default:
		return jsonErrorf("unknown tool: %s", call.Function.Name)
	}
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

func (t *ToolExecutor) executeBash(ctx context.Context, argsJSON string) string {
	var args bashArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Command == "" {
		return `{"error": "command is required"}`
	}

	payload := sandbox.Payload{
		TaskID:        "tool",
		ProjectID:     "",
		WorkspacePath: t.workspacePath,
		Command:       args.Command,
		EnvVars:       t.envVars,
		WallTimeout:   t.wallTimeout,
	}

	result, err := t.sandbox.Execute(ctx, payload)
	if err != nil {
		return jsonErrorf("execution failed: %v", err)
	}

	if !result.Success {
		return jsonErrorf("command failed with exit code %d: %s %s", result.ExitCode, result.Stdout, result.Stderr)
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

func (t *ToolExecutor) executeRead(argsJSON string) string {
	var args readArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Path == "" {
		return `{"error": "path is required"}`
	}

	fullPath, err := t.resolvePath(args.Path, false)
	if err != nil {
		return jsonErrorf("%v", err)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return jsonErrorf("stat failed: %v", err)
	}
	if info.Size() > maxToolReadFileBytes {
		return jsonErrorf("file too large: %d bytes (max %d)", info.Size(), maxToolReadFileBytes)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return jsonErrorf("read failed: %v", err)
	}

	return string(content)
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *ToolExecutor) executeWrite(argsJSON string) string {
	var args writeArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}

	if args.Path == "" {
		return `{"error": "path is required"}`
	}

	fullPath, err := t.resolvePath(args.Path, true)
	if err != nil {
		return jsonErrorf("%v", err)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return jsonErrorf("create directory failed: %v", err)
	}

	if err := os.WriteFile(fullPath, []byte(args.Content), 0644); err != nil {
		return jsonErrorf("write failed: %v", err)
	}

	return `{"success": true}`
}

func (t *ToolExecutor) resolvePath(relPath string, forWrite bool) (string, error) {
	clean := filepath.Clean(relPath)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	workspaceRoot, err := filepath.EvalSymlinks(t.workspacePath)
	if err != nil {
		return "", fmt.Errorf("workspace path is invalid: %w", err)
	}
	workspaceRoot = filepath.Clean(workspaceRoot)

	candidate := filepath.Clean(filepath.Join(t.workspacePath, clean))

	if forWrite {
		parentReal, err := evalExistingAncestor(filepath.Dir(candidate))
		if err != nil {
			return "", fmt.Errorf("failed to resolve parent directory: %w", err)
		}
		if !isWithinRoot(workspaceRoot, parentReal) {
			return "", fmt.Errorf("path escapes workspace")
		}
		return candidate, nil
	}

	targetReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	targetReal = filepath.Clean(targetReal)
	if !isWithinRoot(workspaceRoot, targetReal) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return targetReal, nil
}

func evalExistingAncestor(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func jsonErrorf(format string, args ...any) string {
	payload, err := json.Marshal(map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
	if err != nil {
		return `{"error":"failed to encode error payload"}`
	}
	return string(payload)
}


