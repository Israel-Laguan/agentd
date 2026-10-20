package hooks

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"agentd/internal/models"
)

// ToolCallRecorder stores one row per tool dispatch (the kanban store).
type ToolCallRecorder interface {
	InsertToolCall(ctx context.Context, c models.ToolCall) error
}

// ToolUseHook returns a PostHook that records each tool call's name, file path,
// outcome and result size for the tool use report. Unlike the TOOL_CALL events
// it keeps the whole path and survives event curation. It never alters the
// result and fails open.
func ToolUseHook(rec ToolCallRecorder) PostHook {
	return PostHook{
		Name:   "tool_use",
		Policy: FailOpen,
		Fn: func(ctx HookContext, result string) (string, error) {
			if rec == nil || ctx.SessionID == "" {
				return result, nil
			}
			emitCtx := context.Background()
			if ctx.ExecCtx != nil {
				emitCtx = context.WithoutCancel(ctx.ExecCtx)
			}
			call := models.ToolCall{
				At: time.Now().UTC(), TaskID: ctx.SessionID, CallID: ctx.CallID, Tool: ctx.ToolName,
				Path: toolPath(ctx.Args), Status: toolUseStatus(ctx), ResultBytes: len(result),
			}
			if err := rec.InsertToolCall(emitCtx, call); err != nil {
				slog.Warn("tool call not recorded", "tool", ctx.ToolName, "error", err)
			}
			return result, nil
		},
	}
}

func toolUseStatus(ctx HookContext) string {
	if ctx.ResultStatusSet && ctx.ResultStatus != ToolStatusSuccess {
		return "failed"
	}
	return "ok"
}

// toolPath returns the "path" argument of a file tool, or "" when there is none.
func toolPath(args string) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return ""
	}
	return a.Path
}
