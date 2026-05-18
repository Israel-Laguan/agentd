package worker

import (
	"context"
	"database/sql"
	"encoding/json"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// maxArgumentsSummaryLength is the maximum length for arguments_summary in TOOL_CALL events.
const maxArgumentsSummaryLength = 200

// maxOutputSummaryLength is the maximum length for output_summary in TOOL_RESULT events.
const maxOutputSummaryLength = 1000

// truncationSuffix is appended to truncated summaries.
const truncationSuffix = "...[truncated]"

// ToolCallEvent represents the payload for TOOL_CALL events.
type ToolCallEvent struct {
	ToolName         string `json:"tool_name"`
	CallID           string `json:"call_id"`
	ArgumentsSummary string `json:"arguments_summary"`
}

// emitToolCall emits a TOOL_CALL event with the tool name, call ID, and scrubbed arguments summary.
// It applies scrubbing to the arguments using the worker's configured scrubber, then truncates
// the result to maxArgumentsSummaryLength (200 characters).
func (w *Worker) emitToolCall(ctx context.Context, task models.Task, call gateway.ToolCall) {
	if w.sink == nil {
		return
	}

	// Scrub the arguments using the worker's scrubber
	argumentsSummary := call.Function.Arguments
	if w.sandboxScrubber != nil {
		argumentsSummary = w.sandboxScrubber.Scrub(argumentsSummary)
	}

	// Truncate to maxArgumentsSummaryLength (200 characters)
	argumentsSummary = truncateToMax(argumentsSummary, maxArgumentsSummaryLength)

	event := ToolCallEvent{
		ToolName:         call.Function.Name,
		CallID:           call.ID,
		ArgumentsSummary: argumentsSummary,
	}

	// Use JSON marshaling to ensure proper escaping and structural integrity
	eventData, _ := json.Marshal(event)
	payload := string(eventData)

	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeToolCall,
		Payload:   payload,
	})
}

// ToolResultEvent represents the payload for TOOL_RESULT events.
type ToolResultEvent struct {
	ToolName      string `json:"tool_name"`
	CallID        string `json:"call_id"`
	ExitCode      int    `json:"exit_code"`
	DurationMs    int64  `json:"duration_ms"`
	OutputSummary string `json:"output_summary"`
	StdoutBytes   int    `json:"stdout_bytes"`
	StderrBytes   int    `json:"stderr_bytes"`
}

// truncateToMax truncates the input string to maxLength, appending truncationSuffix if truncation occurred.
func truncateToMax(input string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}

	runes := []rune(input)
	if len(runes) <= maxLength {
		return input
	}

	suffixRunes := []rune(truncationSuffix)
	truncLen := maxLength - len(suffixRunes)
	if truncLen < 0 {
		truncLen = 0
	}
	if truncLen == 0 {
		return string(suffixRunes[:maxLength])
	}

	return string(runes[:truncLen]) + truncationSuffix
}

// emitToolResult emits a TOOL_RESULT event from a structured ToolResult.
func (w *Worker) emitToolResult(ctx context.Context, task models.Task, call gateway.ToolCall, tr ToolResult) {
	if w.sink == nil {
		return
	}

	exitCode := toolResultExitCode(tr)
	outputSummary := tr.Content

	// Scrub output_summary before truncation
	if w.sandboxScrubber != nil {
		outputSummary = w.sandboxScrubber.Scrub(outputSummary)
	}
	outputSummary = truncateToMax(outputSummary, maxOutputSummaryLength)

	var stdoutBytes, stderrBytes int
	if env, err := parseToolEnv(tr.Content); err == nil && env != nil {
		if env.Stdout != "" || env.Stderr != "" || env.Success != nil {
			stdoutBytes = len(env.Stdout)
			stderrBytes = len(env.Stderr)
		} else {
			stdoutBytes = len(tr.Content)
		}
	} else {
		stdoutBytes = len(tr.Content)
	}

	event := ToolResultEvent{
		ToolName:      call.Function.Name,
		CallID:        call.ID,
		ExitCode:      exitCode,
		DurationMs:    tr.ElapsedMs,
		OutputSummary: outputSummary,
		StdoutBytes:   stdoutBytes,
		StderrBytes:   stderrBytes,
	}

	eventData, _ := json.Marshal(event)
	payload := string(eventData)

	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeToolResult,
		Payload:   payload,
	})
}

// toolResultExitCode maps a ToolResult status to a conventional exit code.
func toolResultExitCode(tr ToolResult) int {
	switch tr.Status {
	case ToolStatusSuccess:
		return 0
	case ToolStatusError, ToolStatusTimeout, ToolStatusVetoed, ToolStatusFatal:
		return -1
	default:
		return parseToolExitCode(tr.Content)
	}
}
