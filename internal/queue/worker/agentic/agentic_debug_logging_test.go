package agentic

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"agentd/internal/api/correlation"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// captureDebugLogs redirects the global slog logger to a buffer for the
// duration of the test, at the given level, then restores the previous
// default logger.
func captureDebugLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

var correlationIDPattern = regexp.MustCompile(`correlation_id=(\S+)`)

// correlationIDOn returns the correlation_id carried by the first log line
// containing marker, or "" when that line carries none.
func correlationIDOn(logs, marker string) string {
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		if m := correlationIDPattern.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		return ""
	}
	return ""
}

// debugLoggingHost is a minimal Host for exercising the agentic loop's debug
// logging: it dispatches tool calls via a real ToolExecutor/sandbox path
// (through the default DispatchToolWithHooks behavior is bypassed here in
// favor of a direct sandbox stub) and records the committed terminal text.
type debugLoggingHost struct {
	*noopHost
	committed string
}

func (h *debugLoggingHost) CommitTextWithProfile(_ context.Context, _ models.Task, text string, _ *models.AgentProfile) {
	h.committed = text
}

// buildDebugLoggingTurnLoopInput assembles a runnable agenticTurnLoopInput
// against a real Engine, mirroring buildTopicDriftAgenticTurnLoopInput in
// topic_guard_integration_test.go but without the topic-guard machinery,
// for tests that only need lifecycle/correlation-id log assertions.
func buildDebugLoggingTurnLoopInput(t *testing.T, ctx context.Context, e *Engine, task models.Task, store models.KanbanStore) agenticTurnLoopInput {
	t.Helper()
	project := models.Project{BaseEntity: models.BaseEntity{ID: task.ProjectID}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{ID: task.AgentID, Provider: "openai", Model: "gpt-4", AgenticMode: true}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do the task"},
	}
	sessionMgr := NewSessionManager(task.ID, "do the task", e.config.CheckpointStore)
	cmCfg := config.AgenticContextConfig{
		RollingThresholdTurns: 10, KeepRecentTurns: 5,
		AnchorBudget: 1000, WorkingBudget: 1000, CompressedBudget: 1000,
	}
	cm := agentcontext.NewContextManager(cmCfg, e.config.Gateway, task.AgentID, task.ID)
	goalTracker := agentcontext.NewGoalTracker(task.ID, task.ProjectID, agentcontext.WithCriteriaStore(store))
	ctxBudgetGuard := agentruntime.NewContextBudgetGuard(cm.TotalBudget(), 0.9)
	taskToolExecutor := agenttools.NewToolExecutor(
		e.config.Sandbox, project.WorkspacePath, agenttools.BuildSandboxEnv(nil, nil), 0,
	)
	taskHooks, taskCaps := e.host.MountAgenticHooks(project, profile)
	tools, toolToAdapter := e.host.AgenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)
	return agenticTurnLoopInput{
		ctx: ctx, task: task, project: project, profile: profile,
		messages: &messages, tools: tools, toolToAdapter: toolToAdapter,
		taskToolExecutor: taskToolExecutor,
		iterationGuard:   agentruntime.NewIterationGuard(10),
		budgetGuard:      agentruntime.NewBudgetGuard(nil, task.ID),
		deadlineGuard:    agentruntime.NewDeadlineGuard(ctx),
		ctxBudgetGuard:   ctxBudgetGuard,
		cm:               cm,
		goalTracker:      goalTracker,
		taskHooks:        taskHooks,
		taskCaps:         taskCaps,
		toolTracker:      agenttools.NewToolFailureTracker(0),
		sessionMgr:       sessionMgr,
	}
}

// TestAgenticLoop_CorrelationIDConsistentAcrossLifecycle verifies that a
// correlation ID carried on ctx is present, identically, on every debug log
// line emitted across a full turn: turn start/end, LLM call start/end, tool
// dispatch start/end, and the terminal result (T-022 AC: "agentic loop logs
// task ID, turn lifecycle, tool dispatch, and terminal result"; US-006 AC:
// "one identifier").
func TestAgenticLoop_CorrelationIDConsistentAcrossLifecycle(t *testing.T) {
	const wantID = "trace-agentic-xyz"
	now := time.Now()
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-agentic-1", UpdatedAt: now},
		ProjectID:  "project-agentic-1",
		AgentID:    "agent-1",
		Title:      "run a command",
	}
	store := testutil.NewFakeStore()
	secretArg := "secret-tool-arg-should-not-leak"
	seq := &sequenceGateway{responses: []gateway.AIResponse{
		{
			Content: "running a command",
			ToolCalls: []gateway.ToolCall{{
				ID: "call_1", Type: "function",
				Function: gateway.ToolCallFunction{Name: "bash", Arguments: secretArg},
			}},
		},
		{Content: "terminal-canary-content"},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		secretArg: {Success: true, ExitCode: 0, Stdout: "ok\n"},
	}}
	host := &debugLoggingHost{noopHost: &noopHost{}}
	e := NewEngine(Config{Store: store, Gateway: seq, Sandbox: sb}, host)

	ctx := correlation.WithID(context.Background(), wantID)
	in := buildDebugLoggingTurnLoopInput(t, ctx, e, task, store)

	buf := captureDebugLogs(t, slog.LevelDebug)
	result, reported := e.runAgenticTurnLoop(in)
	logs := buf.String()

	if !reported {
		t.Fatalf("expected the loop to report a terminal result; logs=%s", logs)
	}
	if result.Status != agentruntime.LoopSuccessfulCompletion {
		t.Fatalf("result status = %v, want successful completion; logs=%s", result.Status, logs)
	}
	if host.committed == "" {
		t.Fatal("expected committed terminal text")
	}

	assertLifecycleCorrelationIDs(t, logs, wantID)

	if strings.Contains(logs, secretArg) {
		t.Fatalf("logs leaked tool call argument %q; logs=%s", secretArg, logs)
	}
}

// assertLifecycleCorrelationIDs checks each required lifecycle line on its
// own line. Counting tagged lines separately would let an unrelated debug line
// satisfy the count while the boundary that matters lost its id.
func assertLifecycleCorrelationIDs(t *testing.T, logs, wantID string) {
	t.Helper()
	for _, marker := range []string{
		"agentic: turn start",
		"agentic: turn end",
		"agentic: iteration start",
		"agentic: llm call start",
		"agentic: llm call end",
		"agentic: tool dispatch start",
		"agentic: tool dispatch end",
		"agentic: terminal result",
	} {
		if !strings.Contains(logs, marker) {
			t.Errorf("missing lifecycle log %q; logs=%s", marker, logs)
			continue
		}
		if id := correlationIDOn(logs, marker); id != wantID {
			t.Errorf("%q line correlation_id = %q, want %q; logs=%s", marker, id, wantID, logs)
		}
	}
}

// TestAgenticLoop_DebugLogsSuppressedAtDefaultLevel confirms the agentic
// loop stays silent at the default (info) log level.
func TestAgenticLoop_DebugLogsSuppressedAtDefaultLevel(t *testing.T) {
	now := time.Now()
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-agentic-2", UpdatedAt: now},
		ProjectID:  "project-agentic-2",
		AgentID:    "agent-1",
		Title:      "run a command",
	}
	store := testutil.NewFakeStore()
	seq := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "done, no tools needed"},
	}}
	host := &debugLoggingHost{noopHost: &noopHost{}}
	e := NewEngine(Config{Store: store, Gateway: seq, Sandbox: &mockAgenticSandbox{}}, host)

	ctx := correlation.WithID(context.Background(), "trace-agentic-silent")
	in := buildDebugLoggingTurnLoopInput(t, ctx, e, task, store)

	buf := captureDebugLogs(t, slog.LevelInfo)
	_, reported := e.runAgenticTurnLoop(in)
	logs := buf.String()

	if !reported {
		t.Fatalf("expected the loop to report a terminal result; logs=%s", logs)
	}
	for _, marker := range []string{
		"agentic: turn start", "agentic: turn end", "agentic: llm call start",
		"agentic: llm call end", "agentic: terminal result",
	} {
		if strings.Contains(logs, marker) {
			t.Fatalf("debug marker %q leaked at info level; logs=%s", marker, logs)
		}
	}
}
