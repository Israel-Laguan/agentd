package worker

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type workerResponse struct {
	Command    string          `json:"command,omitempty"`
	TooComplex bool            `json:"too_complex,omitempty"`
	Subtasks   []workerSubtask `json:"subtasks,omitempty"`
}

type workerSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func memoryFormatLessons(memories []models.Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("LESSONS LEARNED (from previous tasks):\n")
	for i, m := range memories {
		if m.Scope == "USER_PREFERENCE" {
			continue
		}
		fmt.Fprintf(&b, "%d. Symptom: %s\n   Solution: %s\n", i+1, m.Symptom.String, m.Solution.String)
	}
	return b.String()
}

// legacyJSONCommandSystemSentinel matches the non-agentic JSON-command worker system prompt.
const legacyJSONCommandSystemSentinel = "Return JSON with either one safe shell command"

func isLegacyJSONCommandSystemPrompt(content string) bool {
	return strings.Contains(content, legacyJSONCommandSystemSentinel)
}

func isMemoryLessonsSystem(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "LESSONS LEARNED")
}

func workerMessages(task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	system := legacyJSONCommandSystemSentinel + `, {"command":"..."}, or if the task is too complex for one command, {"too_complex":true,"subtasks":[{"title":"...","description":"..."}]}.
Only use subtasks when they are smaller, independently executable units of work. Always use non-interactive flags. Examples: -y, --yes, --assume-yes, --non-interactive, DEBIAN_FRONTEND=noninteractive for apt. Never generate commands that prompt for user input, confirmation, or passwords. Never use sudo or run commands requiring root privileges.`
	if profile.SystemPrompt.Valid {
		system = profile.SystemPrompt.String
	}
	user := fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
	return []gateway.PromptMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}
}

func (w *Worker) payload(task models.Task, project models.Project, command string) sandbox.Payload {
	return sandbox.Payload{
		TaskID:        task.ID,
		ProjectID:     task.ProjectID,
		WorkspacePath: project.WorkspacePath,
		Command:       command,
		EnvVars:       BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		WallTimeout:   w.sandboxWallTimeout,
	}
}

// BuildSandboxEnv assembles environment variable pairs for sandbox execution.
func BuildSandboxEnv(allowlist, extra []string) []string {
	allowed := map[string]struct{}{}
	for _, key := range allowlist {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	env := make([]string, 0, len(allowed)+len(extra))
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if _, ok := allowed[parts[0]]; ok {
			env = append(env, pair)
		}
	}
	for _, pair := range extra {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		env = append(env, pair)
	}
	return env
}

func (w *Worker) recoverPanic(ctx context.Context, task models.Task) {
	if recovered := recover(); recovered != nil {
		w.emit(ctx, task, "PANIC", fmt.Sprintf("worker panic: %v", recovered))
		w.failHard(ctx, task, fmt.Errorf("worker panic: %v", recovered))
	}
}

func (w *Worker) startHeartbeat(ctx context.Context, taskID string) func() {
	heartbeatCtx, stop := context.WithCancel(ctx)
	var hbWg sync.WaitGroup
	hbWg.Add(1)
	go func() {
		defer hbWg.Done()
		w.heartbeatLoop(heartbeatCtx, taskID)
	}()
	return func() {
		stop()
		hbWg.Wait()
	}
}

func (w *Worker) emit(ctx context.Context, task models.Task, kind, payload string) {
	if w.sink == nil {
		return
	}
	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventType(kind),
		Payload:   payload,
	})
}

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
	if len(argumentsSummary) > maxArgumentsSummaryLength {
		argumentsSummary = argumentsSummary[:maxArgumentsSummaryLength]
	}

	event := ToolCallEvent{
		ToolName:         call.Function.Name,
		CallID:           call.ID,
		ArgumentsSummary: argumentsSummary,
	}

	payload := fmt.Sprintf("tool_name=%s call_id=%s arguments_summary=%s",
		event.ToolName,
		event.CallID,
		event.ArgumentsSummary,
	)

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
	if len(input) <= maxLength {
		return input
	}
	// Ensure we have room for the truncation suffix
	truncLen := maxLength - len(truncationSuffix)
	if truncLen < 0 {
		truncLen = 0
	}
	return input[:truncLen] + truncationSuffix
}

// emitToolResult emits a TOOL_RESULT event with the tool name, call ID, exit code, duration,
// output summary, and byte counts. It applies truncation to the output_summary to
// maxOutputSummaryLength (1000 characters).
func (w *Worker) emitToolResult(ctx context.Context, task models.Task, call gateway.ToolCall, result string, durationMs int64) {
	if w.sink == nil {
		return
	}

	// Determine exit code and output summary
	var exitCode int
	var outputSummary string

	// Try to parse the result as a JSON error or execution result
	if strings.HasPrefix(result, `{"error"`) || strings.HasPrefix(result, `{"FatalError"`) {
		// Tool execution failed - exit code -1
		exitCode = -1
		outputSummary = result
	} else if strings.HasPrefix(result, `{"Success":false`) {
		// ExecutionResult with Success=false
		exitCode = -1
		outputSummary = result
	} else {
		// Success case - use ExitCode from ExecutionResult if available, otherwise 0
		exitCode = 0

		// Try to extract ExitCode from the result JSON
		if strings.Contains(result, `"ExitCode"`) {
			// Use simple string extraction for ExitCode
			if idx := strings.Index(result, `"ExitCode"`); idx != -1 {
				// Look for number after "ExitCode":
				rest := result[idx+len(`"ExitCode"`):]
				rest = strings.TrimSpace(rest)
				if len(rest) > 0 && rest[0] == ':' {
					rest = strings.TrimSpace(rest[1:])
					// Extract number
					for i, c := range rest {
						if c >= '0' && c <= '9' {
							// Try to parse the number
							var n int
							for j := i; j < len(rest); j++ {
								if rest[j] >= '0' && rest[j] <= '9' {
									n = n*10 + int(rest[j]-'0')
								} else {
									break
								}
							}
							if n > 0 {
								exitCode = n
								break
							}
						}
					}
				}
			}
		}

		outputSummary = result
	}

	// Truncate output_summary to maxOutputSummaryLength (1000 characters)
	outputSummary = truncateToMax(outputSummary, maxOutputSummaryLength)

	// Calculate original byte counts (before truncation)
	stdoutBytes := len(result)
	stderrBytes := 0

	event := ToolResultEvent{
		ToolName:      call.Function.Name,
		CallID:        call.ID,
		ExitCode:      exitCode,
		DurationMs:    durationMs,
		OutputSummary: outputSummary,
		StdoutBytes:   stdoutBytes,
		StderrBytes:   stderrBytes,
	}

	payload := fmt.Sprintf("tool_name=%s call_id=%s exit_code=%d duration_ms=%d output_summary=%s stdout_bytes=%d stderr_bytes=%d",
		event.ToolName,
		event.CallID,
		event.ExitCode,
		event.DurationMs,
		event.OutputSummary,
		event.StdoutBytes,
		event.StderrBytes,
	)

	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeToolResult,
		Payload:   payload,
	})
}
