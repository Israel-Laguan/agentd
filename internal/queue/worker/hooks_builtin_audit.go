package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// AuditHook returns a PostHook that emits TOOL_CALL and TOOL_RESULT
// events through the provided EventSink. It replaces the inline
// emitToolCall / emitToolResult calls that previously lived in the
// agentic loop body, guaranteeing that every tool dispatch is audited
// regardless of call site.
func AuditHook(sink models.EventSink, scrubber sandbox.Scrubber) PostHook {
	return PostHook{
		Name:   "audit",
		Policy: FailOpen,
		Fn: func(ctx HookContext, result string) (string, error) {
			if sink == nil {
				return result, nil
			}
			emitToolCallHook(context.Background(), sink, ctx, scrubber)
			durationMs := time.Since(ctx.Timestamp).Milliseconds()
			emitToolResultHook(context.Background(), sink, ctx, result, durationMs, scrubber)
			return result, nil
		},
	}
}

// emitToolCallHook emits a TOOL_CALL event via the sink using HookContext.
func emitToolCallHook(ctx context.Context, sink models.EventSink, hctx HookContext, scrubber sandbox.Scrubber) {
	argumentsSummary := hctx.Args
	if scrubber != nil {
		argumentsSummary = scrubber.Scrub(argumentsSummary)
	}
	argumentsSummary = truncateToMax(argumentsSummary, maxArgumentsSummaryLength)

	event := ToolCallEvent{
		ToolName:         hctx.ToolName,
		CallID:           hctx.CallID,
		ArgumentsSummary: argumentsSummary,
	}
	eventData, _ := json.Marshal(event)
	_ = sink.Emit(ctx, models.Event{
		ProjectID: hctx.ProjectID,
		TaskID:    sql.NullString{String: hctx.SessionID, Valid: hctx.SessionID != ""},
		Type:      models.EventTypeToolCall,
		Payload:   string(eventData),
	})
}

// emitToolResultHook emits a TOOL_RESULT event via the sink using HookContext.
func emitToolResultHook(ctx context.Context, sink models.EventSink, hctx HookContext, result string, durationMs int64, scrubber sandbox.Scrubber) {
	var exitCode int
	if hctx.ResultStatusSet {
		exitCode = toolResultExitCode(ToolResult{Status: hctx.ResultStatus, Content: result})
	} else {
		exitCode = parseToolExitCode(result)
	}
	outputSummary := result
	if scrubber != nil {
		outputSummary = scrubber.Scrub(outputSummary)
	}
	outputSummary = truncateToMax(outputSummary, maxOutputSummaryLength)

	var stdoutBytes, stderrBytes int
	if env, err := parseToolEnv(result); err == nil && env != nil {
		if env.Stdout != "" || env.Stderr != "" || env.Success != nil {
			stdoutBytes = len(env.Stdout)
			stderrBytes = len(env.Stderr)
		} else {
			stdoutBytes = len(result)
		}
	} else {
		stdoutBytes = len(result)
	}

	event := ToolResultEvent{
		ToolName:      hctx.ToolName,
		CallID:        hctx.CallID,
		ExitCode:      exitCode,
		DurationMs:    durationMs,
		OutputSummary: outputSummary,
		StdoutBytes:   stdoutBytes,
		StderrBytes:   stderrBytes,
	}
	eventData, _ := json.Marshal(event)
	_ = sink.Emit(ctx, models.Event{
		ProjectID: hctx.ProjectID,
		TaskID:    sql.NullString{String: hctx.SessionID, Valid: hctx.SessionID != ""},
		Type:      models.EventTypeToolResult,
		Payload:   string(eventData),
	})
}
