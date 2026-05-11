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
	toolNameBash  = "bash"
	toolNameRead  = "read"
	toolNameWrite = "write"
)

type ToolExecutor struct {
	sandbox          sandbox.Executor
	workspacePath    string
	envVars          []string
	wallTimeout      time.Duration
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
		return fmt.Sprintf(`{"error": "unknown tool: %s"}`, call.Function.Name)
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
		return fmt.Sprintf(`{"error": "invalid arguments: %v"}`, err)
	}

	if args.Command == "" {
		return `{"error": "command is required"}`
	}

	if isDangerous(args.Command) {
		return `{"error": "command blocked: dangerous operation"}`
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
		return fmt.Sprintf(`{"error": "execution failed: %v}`, err)
	}

	if !result.Success {
		return fmt.Sprintf(`{"error": "command failed with exit code %d: %s %s}`, result.ExitCode, result.Stdout, result.Stderr)
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
		return fmt.Sprintf(`{"error": "invalid arguments: %v"}`, err)
	}

	if args.Path == "" {
		return `{"error": "path is required"}`
	}

	fullPath, err := t.resolvePath(args.Path)
	if err != nil {
		return fmt.Sprintf(`{"error": "%v}`, err)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf(`{"error": "read failed: %v}`, err)
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
		return fmt.Sprintf(`{"error": "invalid arguments: %v"}`, err)
	}

	if args.Path == "" {
		return `{"error": "path is required"}`
	}

	fullPath, err := t.resolvePath(args.Path)
	if err != nil {
		return fmt.Sprintf(`{"error": "%v}`, err)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf(`{"error": "create directory failed: %v}`, err)
	}

	if err := os.WriteFile(fullPath, []byte(args.Content), 0644); err != nil {
		return fmt.Sprintf(`{"error": "write failed: %v}`, err)
	}

	return `{"success": true}`
}

func (t *ToolExecutor) resolvePath(relPath string) (string, error) {
	clean := filepath.Clean(relPath)
	if strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return filepath.Join(t.workspacePath, clean), nil
}

func isDangerous(cmd string) bool {
	lower := strings.ToLower(cmd)
	dangerous := []string{
		"sudo",
		"su -",
		"rm -rf /",
		"mkfs",
		"dd if=",
		":(){:|:&};:",
	}
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}
