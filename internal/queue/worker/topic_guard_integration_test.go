package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type driftTrackingGateway struct {
	topicDriftGateway
}

// driftThenSequenceGateway returns YES for topic-guard drift checks and otherwise
// delegates to a sequenceGateway for agentic turn generation.
type driftThenSequenceGateway struct {
	seq        *sequenceGateway
	driftCalls int
}

func isTopicGuardRequest(req gateway.AIRequest) bool {
	if req.Role == gateway.RoleMemory {
		return true
	}
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "classify whether two messages") {
			return true
		}
	}
	return false
}

func (g *driftThenSequenceGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if isTopicGuardRequest(req) {
		g.driftCalls++
		return gateway.AIResponse{Content: "YES"}, nil
	}
	return g.seq.Generate(ctx, req)
}

func (g *driftThenSequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (g *driftThenSequenceGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

type topicDriftTurnLoopFixture struct {
	w         *Worker
	gw        *driftThenSequenceGateway
	seq       *sequenceGateway
	committed *string
	in        agenticTurnLoopInput
}

func newTopicDriftTurnLoopWorker(
	t *testing.T,
) (*Worker, *driftThenSequenceGateway, *sequenceGateway, *string, models.Task) {
	t.Helper()
	now := time.Now().UTC()
	committed := ""
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: now},
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		Title:      "CSS styling",
	}
	store := &mockCommitStore{
		text: &committed,
		task: &task,
		comments: []models.Comment{{
			BaseEntity: models.BaseEntity{UpdatedAt: now},
			TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "database migrations",
		}},
	}
	seq := &sequenceGateway{responses: []gateway.AIResponse{
		{
			Content: "running a command",
			ToolCalls: []gateway.ToolCall{{
				ID: "call_1", Type: "function",
				Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "echo ok"}`},
			}},
		},
		{Content: "task completed after topic drift rewind"},
	}}
	gw := &driftThenSequenceGateway{seq: seq}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo ok": {Success: true, ExitCode: 0, Stdout: "ok\n"},
	}}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	return w, gw, seq, &committed, task
}

func buildTopicDriftAgenticTurnLoopInput(
	t *testing.T, w *Worker, task models.Task, messages *[]gateway.PromptMessage,
) agenticTurnLoopInput {
	t.Helper()
	project := models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	sessionMgr := NewSessionManager(task.ID, "CSS styling", w.checkpointStore)
	cm, goalTracker := w.newAgenticContextManager(task)
	contextBudget := cm.TotalBudget()
	ctxBudgetGuard := NewContextBudgetGuard(contextBudget, w.contextWarningThreshold)
	taskToolExecutor := w.newAgenticTaskToolExecutor(project, task)
	taskHooks, taskCaps := w.mountAgenticHooks(project, profile)
	tools, toolToAdapter := w.agenticToolsWithExtras(context.Background(), taskToolExecutor, taskCaps)
	return agenticTurnLoopInput{
		ctx:              context.Background(),
		task:             task,
		project:          project,
		profile:          profile,
		messages:         messages,
		tools:            tools,
		toolToAdapter:    toolToAdapter,
		taskToolExecutor: taskToolExecutor,
		iterationGuard:   NewIterationGuard(10),
		budgetGuard:      NewBudgetGuard(nil, task.ID),
		deadlineGuard:    NewDeadlineGuard(context.Background()),
		ctxBudgetGuard:   ctxBudgetGuard,
		cm:               cm,
		goalTracker:      goalTracker,
		taskHooks:        taskHooks,
		taskCaps:         taskCaps,
		toolTracker:      newToolFailureTracker(0),
		sessionMgr:       sessionMgr,
	}
}

func setupTopicDriftTurnLoopRewindFixture(t *testing.T) topicDriftTurnLoopFixture {
	t.Helper()
	w, gw, seq, committed, task := newTopicDriftTurnLoopWorker(t)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling"},
		{Role: "assistant", Content: "styled"},
	}
	return topicDriftTurnLoopFixture{
		w: w, gw: gw, seq: seq, committed: committed,
		in: buildTopicDriftAgenticTurnLoopInput(t, w, task, &messages),
	}
}

func assertTopicDriftTurnLoopContinuesAfterRewind(
	t *testing.T, f topicDriftTurnLoopFixture, result LoopResult, reported bool,
) {
	t.Helper()
	if !reported {
		t.Fatal("expected loop to report a result after continuing past topic drift")
	}
	if result.Status != LoopSuccessfulCompletion {
		t.Fatalf("result status = %v, want successful completion", result.Status)
	}
	if f.gw.driftCalls < 1 {
		t.Fatal("expected topic drift guard to call gateway")
	}
	if f.seq.callCount < 2 {
		t.Fatalf("expected at least 2 agentic gateway calls after drift rewind, got %d", f.seq.callCount)
	}
	for _, m := range *f.in.messages {
		if m.Role == "assistant" && m.Content == "styled" {
			t.Fatal("assistant history from before drift should be cleared after rewind")
		}
	}
	if *f.committed == "" {
		t.Fatal("expected committed text after loop completion")
	}
}

func TestGuardTopicDrift_SkipsFirstTurn(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	w := NewWorker(nil, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "CSS styling", nil)

	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}
	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 0, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no reset on turn 0")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no drift gateway calls on turn 0, got %d", gw.calls)
	}
}

func TestGuardTopicDrift_NoNewComment_SkipsDriftCall(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	w := NewWorker(testutil.NewFakeStore(), gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "CSS styling", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}

	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no reset without new comment")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.calls)
	}
}

func TestGuardTopicDrift_DriftBeforeCompression(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "database migrations",
	})

	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
		AgenticContext: config.AgenticContextConfig{
			RollingThresholdTurns: 1,
			KeepRecentTurns:       1,
		},
	})
	sm := NewSessionManager("task-1", "CSS styling", w.checkpointStore)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling"},
		{Role: "assistant", Content: "styled"},
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}

	reset, err := w.guardTopicDrift(context.Background(), task, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if !reset || !errors.Is(err, errTopicDriftReset) {
		t.Fatalf("guardTopicDrift = (%v, %v), want (true, errTopicDriftReset)", reset, err)
	}
	// Drift reset runs before prepareAgenticIteration would compress the abandoned session.
	if strings.Contains(messages[0].Content, "PREVIOUS CONTEXT SUMMARY") && len(messages) <= 2 {
		t.Fatal("fresh session should not include compressed summary in short message list")
	}
	for _, m := range messages {
		if m.Role == "assistant" {
			t.Fatal("assistant history should be cleared after drift")
		}
	}
}

func TestRunAgenticTurnLoop_TopicDriftErrWithRewindContinues(t *testing.T) {
	t.Parallel()
	f := setupTopicDriftTurnLoopRewindFixture(t)
	result, reported := f.w.runAgenticTurnLoop(f.in)
	assertTopicDriftTurnLoopContinuesAfterRewind(t, f, result, reported)
}

func TestGuardTopicDrift_RelatedFollowUp_NoReset(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "NO"}}
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "now add tests for the migration",
	})
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "database migrations", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "database migrations"}}

	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no session reset for related follow-up")
	}
	if sm.TopicAnchor() != "now add tests for the migration" {
		t.Fatalf("topic anchor = %q, want updated follow-up", sm.TopicAnchor())
	}
}
