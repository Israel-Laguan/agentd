package worker

import (
	"context"
	"database/sql"
	"encoding/json"
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

// emitToolResult emits a TOOL_RESULT event with the tool name, call ID, exit code, duration,
// output summary, and byte counts. It applies truncation to the output_summary to
// maxOutputSummaryLength (1000 characters).
func (w *Worker) emitToolResult(ctx context.Context, task models.Task, call gateway.ToolCall, result string, durationMs int64) {
	if w.sink == nil {
		return
	}

	exitCode := parseToolExitCode(result)
	outputSummary := result

	// Scrub output_summary before truncation
	if w.sandboxScrubber != nil {
		outputSummary = w.sandboxScrubber.Scrub(outputSummary)
	}
	// Truncate output_summary to maxOutputSummaryLength (1000 characters)
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
		ToolName:      call.Function.Name,
		CallID:        call.ID,
		ExitCode:      exitCode,
		DurationMs:    durationMs,
		OutputSummary: outputSummary,
		StdoutBytes:   stdoutBytes,
		StderrBytes:   stderrBytes,
	}

	// Use JSON marshaling to ensure proper escaping and structural integrity
	eventData, _ := json.Marshal(event)
	payload := string(eventData)

	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeToolResult,
		Payload:   payload,
	})
}

// toolExecEnvelope is used to parse tool execution results from JSON.
type toolExecEnvelope struct {
	Success    *bool  `json:"Success"`
	ExitCode   int    `json:"ExitCode"`
	Stdout     string `json:"Stdout"`
	Stderr     string `json:"Stderr"`
	Error      string `json:"error"`
	FatalError string `json:"FatalError"`
}

// parseToolEnv attempts to parse the tool result as a toolExecEnvelope.
// Returns the envelope and nil error if JSON is valid, or nil and the error otherwise.
func parseToolEnv(result string) (*toolExecEnvelope, error) {
	var env toolExecEnvelope
	if err := json.Unmarshal([]byte(result), &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// parseToolExitCode determines the exit code from a tool result string.
// Returns -1 for errors or failed executions, 0 otherwise.
func parseToolExitCode(result string) int {
	env, err := parseToolEnv(result)
	if err != nil {
		// Fallback behavior for non-JSON tool results
		if strings.HasPrefix(result, `{"error"`) || strings.HasPrefix(result, `{"FatalError"`) {
			return -1
		}
		if strings.HasPrefix(result, `{"Success":false`) {
			return -1
		}
		return 0
	}
	// Successfully parsed JSON
	if env.Error != "" || env.FatalError != "" || (env.Success != nil && !*env.Success) {
		return -1
	}
	return env.ExitCode
}