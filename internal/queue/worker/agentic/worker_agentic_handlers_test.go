package agentic

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// captureUpdatedAtHost records the taskUpdatedAt passed to DispatchToolWithHooks.
type captureUpdatedAtHost struct {
	*noopHost
	capturedUpdatedAt time.Time
}

func (h *captureUpdatedAtHost) DispatchToolWithHooks(
	_ context.Context,
	_, _, _ string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	_ map[string]string,
	_ *agenttools.ToolExecutor,
	_ *agenthooks.HookChain,
	_ *capabilities.Registry,
	_ string,
) (agenttools.ToolResult, bool) {
	h.capturedUpdatedAt = taskUpdatedAt
	return agenttools.SuccessResult(call.ID, "ok", 0), false
}

type handlersMockHost struct {
	*noopHost
	committedText *string
}

func (h *handlersMockHost) CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile) {
	if h.committedText != nil {
		*h.committedText = text
	}
}

func (h *handlersMockHost) DispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
	providerName string,
) (agenttools.ToolResult, bool) {
	hookCtx := agenthooks.HookContext{
		ExecCtx:       ctx,
		SessionID:     sessionID,
		ProjectID:     projectID,
		TurnID:        turnID,
		TaskUpdatedAt: taskUpdatedAt,
		ToolName:      call.Function.Name,
		Args:          call.Function.Arguments,
		CallID:        call.ID,
	}
	verdict := taskHooks.RunPre(hookCtx)
	if verdict.Veto {
		return agenttools.VetoedResult(call.ID, verdict.Result), false
	}
	return agenttools.SuccessResult(call.ID, "success", 0), false
}

func (h *handlersMockHost) GenerateRespecifiedUserTurn(
	ctx context.Context,
	task models.Task,
	plan *agentcontext.Plan,
	failing []agentcontext.PlanStep,
	messages []gateway.PromptMessage,
	cm *agentcontext.ContextManager,
	budget *agentruntime.BudgetGuard,
) (string, error) {
	return "", errors.New("respec gateway failure")
}

func (h *handlersMockHost) RepairOutputWithPlan(
	ctx context.Context,
	task models.Task,
	plan *agentcontext.Plan,
	content string,
	budget *agentruntime.BudgetGuard,
) (string, bool) {
	// Simulates plan recovery indicating respec is needed
	return content, true
}

func (h *handlersMockHost) HandleGatewayError(ctx context.Context, task models.Task, err error) {}

type nilGetTaskStore struct {
	models.KanbanStore
}

func (s *nilGetTaskStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	return nil, nil
}

func TestHandleAgenticToolCalls_GetTaskNotFoundDoesNotPanic(t *testing.T) {
	t.Parallel()

	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "shortcut",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, ShortCircuit: true, Result: "cached"}, nil
		},
	})

	store := testutil.NewFakeStore() // returns not found/err on GetTask if not present
	host := &handlersMockHost{noopHost: &noopHost{}}
	e := &Engine{
		config: Config{Store: store},
		host:   host,
	}

	staleAt := time.Now().Add(-time.Hour)
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-missing", UpdatedAt: staleAt},
		ProjectID:  "proj",
	}
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}}}
	ex := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)

	var messages []gateway.PromptMessage
	abort, _, report := e.handleAgenticToolCalls(
		context.Background(), task, "", resp, &messages, nil, ex, taskHooks, nil, cm, agenttools.NewToolFailureTracker(0), 0, agentruntime.NewBudgetGuard(nil, task.ID),
	)
	if abort || report {
		t.Fatalf("abort = %v report = %v, want non-aborting tool dispatch", abort, report)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1 tool result appended", len(messages))
	}
	if !strings.Contains(messages[0].Content, "cached") {
		t.Fatalf("tool message = %q, want cached hook result", messages[0].Content)
	}
}

func TestHandleAgenticToolCalls_GetTaskNilNilDoesNotPanic(t *testing.T) {
	t.Parallel()

	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "shortcut",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, ShortCircuit: true, Result: "cached"}, nil
		},
	})

	store := &nilGetTaskStore{}
	host := &handlersMockHost{noopHost: &noopHost{}}
	e := &Engine{
		config: Config{Store: store},
		host:   host,
	}

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-nil-nil", UpdatedAt: time.Now()},
		ProjectID:  "proj",
	}
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}}}
	ex := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)

	var messages []gateway.PromptMessage
	abort, _, report := e.handleAgenticToolCalls(
		context.Background(), task, "", resp, &messages, nil, ex, taskHooks, nil, cm, agenttools.NewToolFailureTracker(0), 0, agentruntime.NewBudgetGuard(nil, task.ID),
	)
	if abort || report {
		t.Fatalf("abort = %v report = %v, want non-aborting tool dispatch", abort, report)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1 tool result appended", len(messages))
	}
}

// refreshedStore overrides GetTask to return a fresher UpdatedAt than what the task holds.
type refreshedStore struct {
	models.KanbanStore
	freshUpdatedAt time.Time
}

func (s *refreshedStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: id, UpdatedAt: s.freshUpdatedAt}}, nil
}

func TestHandleAgenticToolCalls_RefreshesTaskUpdatedAt(t *testing.T) {
	t.Parallel()

	freshAt := time.Now().Add(time.Hour)
	store := &refreshedStore{freshUpdatedAt: freshAt}
	host := &captureUpdatedAtHost{noopHost: &noopHost{}}
	e := &Engine{
		config: Config{Store: store},
		host:   host,
	}

	staleAt := time.Now().Add(-time.Hour)
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-refresh", UpdatedAt: staleAt},
		ProjectID:  "proj",
	}
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID:       "call-r",
		Type:     "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}}}
	ex := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", task.ID)

	var messages []gateway.PromptMessage
	e.handleAgenticToolCalls(
		context.Background(), task, "", resp, &messages, nil, ex, nil, nil, cm,
		agenttools.NewToolFailureTracker(0), 0, agentruntime.NewBudgetGuard(nil, task.ID),
	)
	if !host.capturedUpdatedAt.Equal(freshAt) {
		t.Fatalf("DispatchToolWithHooks got updatedAt = %v, want refreshed %v", host.capturedUpdatedAt, freshAt)
	}
}

type respecFailGateway struct{}

func (g *respecFailGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	user := ""
	if n := len(req.Messages); n > 0 {
		user = req.Messages[n-1].Content
	}
	if strings.HasPrefix(user, "Revise the user task prompt") {
		return gateway.AIResponse{}, errors.New("respec gateway failure")
	}
	return gateway.AIResponse{Content: "repaired"}, nil
}

func (g *respecFailGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}
func (g *respecFailGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return &gateway.ScopeAnalysis{}, nil
}
func (g *respecFailGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return &gateway.IntentAnalysis{}, nil
}
func (g *respecFailGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func TestFinishAgenticTurnNoTools_RespecFailurePreservesMessages(t *testing.T) {
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-respec-fail"}, ProjectID: "proj", AgentID: "agent"}
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, task.AgentID, task.ID)
	committed := ""
	host := &handlersMockHost{committedText: &committed}
	e := &Engine{
		config: Config{
			Gateway:     &respecFailGateway{},
			PlanningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 1, MaxRedoPasses: 0},
			MessageEditor: agentcontext.NewMessageEditor(wsession.NewMemoryCheckpointStore(), nil, cm),
		},
		host: host,
	}
	plan := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: "only", Action: "do", OutputFormat: "text"}}}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "<!-- step:only -->\n<!-- /step:only -->\n"},
	}
	before := len(messages)
	respecAttempts := 0
	cont, _, report, _, err := e.finishAgenticTurnNoTools(
		context.Background(),
		task,
		models.AgentProfile{},
		"<!-- step:only -->\n<!-- /step:only -->\n",
		plan,
		nil,
		"task-respec-fail:0",
		0, agentruntime.NewBudgetGuard(nil, task.ID), agentruntime.NewContextBudgetGuard(60000, 0), cm,
		&messages,
		&respecAttempts,
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("finishAgenticTurnNoTools: %v", err)
	}
	if cont {
		t.Fatal("expected no rewind when respec fails")
	}
	if !report {
		t.Fatal("expected report on fallback commit path")
	}
	if len(messages) != before {
		t.Fatalf("len(messages) = %d, want %d after failed respec", len(messages), before)
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "agentic respec repair skipped") {
		t.Fatalf("expected respec repair skip warning, logs:\n%s", logs)
	}
	if !strings.Contains(logs, "task-respec-fail") || !strings.Contains(logs, "task-respec-fail:0") {
		t.Fatalf("expected task_id and turn_id in warning, logs:\n%s", logs)
	}
	if !strings.Contains(logs, "respec gateway failure") {
		t.Fatalf("expected respec error in warning, logs:\n%s", logs)
	}
}
