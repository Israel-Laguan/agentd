package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"agentd/internal/sandbox"
)

type bashArgs struct {
	Command string `json:"command"`
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
