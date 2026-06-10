package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// transitionEnforcingStore mirrors production UpdateTaskState validation.
type transitionEnforcingStore struct {
	*testutil.FakeKanbanStore
}

func (s *transitionEnforcingStore) UpdateTaskState(ctx context.Context, id string, expected time.Time, next models.TaskState) (*models.Task, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.UpdatedAt.Equal(expected) {
		return nil, models.ErrStateConflict
	}
	if !task.State.CanTransitionTo(next) {
		return nil, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, task.State, next)
	}
	return s.FakeKanbanStore.UpdateTaskState(ctx, id, expected, next)
}

type elicitationSequenceGateway struct {
	elicitationJSON  string
	requests         []gateway.AIRequest
	elicitationCalls int
	agenticCalls     int
}

func (g *elicitationSequenceGateway) isElicitorRequest(req gateway.AIRequest) bool {
	if !req.JSONMode || req.Role != gateway.RoleMemory {
		return false
	}
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "pre-task ambiguity") {
			return true
		}
	}
	return false
}

func (g *elicitationSequenceGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	if g.isElicitorRequest(req) {
		g.elicitationCalls++
		return gateway.AIResponse{Content: g.elicitationJSON}, nil
	}
	g.agenticCalls++
	return gateway.AIResponse{Content: "done"}, nil
}

func (g *elicitationSequenceGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *elicitationSequenceGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *elicitationSequenceGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (g *elicitationSequenceGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func sampleElicitationJSON() string {
	analysis := ElicitationAnalysis{
		NeedsClarification: true,
		Questions: []ElicitationQuestion{
			{Question: "Which bug are you seeing?", Options: []string{"login", "checkout"}},
			{Question: "Which environment should we target?", Options: []string{"dev", "prod"}},
			{Question: "What is the expected behavior?"},
		},
	}
	b, _ := json.Marshal(analysis)
	return string(b)
}

func TestRequestClarificationFromAgent_RejectsEmptyQuestion(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	w := &Worker{store: store}

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: time.Now()},
		ProjectID:  "proj-1",
	}
	err := w.RequestClarificationFromAgent(context.Background(), task, "   ", nil, "")
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestRequestClarificationFromAgent_CreatesAtomicHandoff(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "clarification-request-proj",
		Tasks:       []models.DraftTask{{Title: "Clarify task", Description: "needs input"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	w := &Worker{store: store}

	err = w.RequestClarificationFromAgent(ctx, *running, "Which environment?", []string{"dev", "prod"}, "Need target before editing.")
	if err != nil {
		t.Fatalf("RequestClarificationFromAgent: %v", err)
	}

	parent, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
	children, err := store.ListChildTasks(ctx, running.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 || !strings.HasPrefix(children[0].Title, models.HITLSubtaskTitleClarification) {
		t.Fatalf("children = %#v, want one clarification subtask", children)
	}
	comments, err := store.ListComments(ctx, running.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
	if comments[0].Author != models.CommentAuthorWorkerAgent {
		t.Fatalf("comment author = %s, want WORKER_AGENT", comments[0].Author)
	}
	expiresAt, ok := parseHITLExpiry(comments)
	if !ok || expiresAt.IsZero() {
		t.Fatalf("expiry ok=%v expires_at=%s; want parsed expiry", ok, expiresAt)
	}
}

func setupAgenticElicitationProfile(t *testing.T, store models.KanbanStore, ctx context.Context) {
	t.Helper()
	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.AgenticMode = true
	profile.Provider = "openai"
	profile.Model = "gpt-4"
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
}

func TestShouldSkipElicitation_FullySpecified(t *testing.T) {
	t.Parallel()
	desc := strings.Repeat("Implement the OAuth callback handler with PKCE. ", 15) +
		"\n- must validate state\n- must use /auth/callback.go\nAcceptance: returns 302 on success."
	task := models.Task{
		Description:     desc,
		SuccessCriteria: []string{"redirects on success"},
	}
	if !shouldSkipElicitation(task) {
		t.Fatal("expected fully specified task to skip elicitation")
	}
}

func TestShouldSkipElicitation_AmbiguousShort(t *testing.T) {
	t.Parallel()
	task := models.Task{Description: "fix the bug"}
	if shouldSkipElicitation(task) {
		t.Fatal("expected ambiguous short task not to skip elicitation")
	}
}

func TestShouldSkipElicitation_StandaloneFilename(t *testing.T) {
	t.Parallel()
	desc := strings.Repeat("Update the entrypoint in main.go to wire the new handler. ", 12)
	task := models.Task{Description: desc}
	if !shouldSkipElicitation(task) {
		t.Fatal("expected task referencing standalone main.go to skip elicitation")
	}
}

func TestShouldSkipElicitation_LongNarrativeDoesNotSkip(t *testing.T) {
	t.Parallel()
	desc := strings.Repeat(
		"We noticed intermittent failures in staging last week. Everything looked fine as expected at first, "+
			"and we should probably investigate when someone has time. The team discussed whether we need "+
			"more logging but did not decide on concrete steps yet. ",
		8,
	)
	if utf8.RuneCountInString(strings.TrimSpace(desc)) <= elicitationSkipMinDescriptionRunes {
		t.Fatalf("test description too short: %d runes", utf8.RuneCountInString(strings.TrimSpace(desc)))
	}
	task := models.Task{Description: desc}
	if shouldSkipElicitation(task) {
		t.Fatal("expected long narrative without explicit constraints not to skip elicitation")
	}
}

func TestElicitor_AnalyzeUsesRoleMemory(t *testing.T) {
	t.Parallel()
	gw := &elicitationSequenceGateway{elicitationJSON: sampleElicitationJSON()}
	elicitor := NewElicitor(gw)
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, Title: "Fix", Description: "fix the bug"}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "p1"}}

	analysis, err := elicitor.Analyze(context.Background(), task, project, "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !analysis.NeedsClarification || len(analysis.Questions) < 3 {
		t.Fatalf("analysis = %+v, want clarification with questions", analysis)
	}
	if gw.elicitationCalls != 1 {
		t.Fatalf("elicitation calls = %d, want 1", gw.elicitationCalls)
	}
	if gw.requests[0].Role != gateway.RoleMemory || !gw.requests[0].JSONMode {
		t.Fatalf("request role=%q json=%v, want memory+json", gw.requests[0].Role, gw.requests[0].JSONMode)
	}
}

func TestProcess_AmbiguousTaskBlocksForElicitation(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticElicitationProfile(t, store, ctx)

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "elicitation-proj",
		Tasks:       []models.DraftTask{{Title: "Fix bug", Description: "fix the bug"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	queued, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	gw := &elicitationSequenceGateway{elicitationJSON: sampleElicitationJSON()}
	w := NewWorker(store, gw, &mockAgenticSandbox{}, nil, nil, WorkerOptions{MaxToolIterations: 3})
	w.Process(ctx, *queued)

	assertAmbiguousElicitationBlocked(t, store, ctx, task.ID, gw)
}

func TestTryConsumeElicitationAnswers_EnrichesDescription(t *testing.T) {
	t.Parallel()
	ctx, store, w, parent, running := setupConsumeElicitationFixture(t)

	enriched, ok, err := w.tryConsumeElicitationAnswers(ctx, *running)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Fatal("expected consumption")
	}
	if !strings.Contains(enriched.Description, clarificationsBlockHeader) {
		t.Fatalf("description missing clarifications header: %q", enriched.Description)
	}
	if !strings.Contains(enriched.Description, "Login timeout on /auth in staging.") {
		t.Fatalf("description missing human answer: %q", enriched.Description)
	}
	if !strings.Contains(enriched.Description, "Which bug?") {
		t.Fatalf("description missing stored questions: %q", enriched.Description)
	}

	persisted, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get persisted: %v", err)
	}
	if !strings.Contains(persisted.Description, clarificationsBlockHeader) {
		t.Fatalf("persisted description missing clarifications block: %q", persisted.Description)
	}
}

func TestProcess_FullySpecifiedSkipsElicitation(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticElicitationProfile(t, store, ctx)

	longDesc := strings.Repeat("Ship the billing webhook with idempotent retries and structured logs. ", 12) +
		"\n- must persist events in postgres\n- must expose /webhooks/billing.go\nAcceptance: returns 200 for valid signatures."
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "specified-proj",
		Tasks: []models.DraftTask{{
			Title:           "Billing webhook",
			Description:     longDesc,
			SuccessCriteria: []string{"valid signature returns 200"},
		}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	queued, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	if !shouldSkipElicitation(*queued) {
		t.Fatalf("queued task should skip elicitation (runes=%d criteria=%d)",
			utf8.RuneCountInString(queued.Description), len(queued.SuccessCriteria))
	}

	gw := &elicitationSequenceGateway{elicitationJSON: sampleElicitationJSON()}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{}}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 3})
	w.Process(ctx, *queued)

	if gw.elicitationCalls != 0 {
		t.Fatalf("elicitation calls = %d, want 0 for fully specified task", gw.elicitationCalls)
	}
	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State == models.TaskStateBlocked {
		t.Fatal("fully specified task should not block for elicitation")
	}
}

func TestRunPreTaskElicitation_PendingChildReblocksRunningParent(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "reblock-proj",
		Tasks:       []models.DraftTask{{Title: "Fix", Description: "fix the bug"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	parent := tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := recordElicitationQuestions(ctx, store, parent.ID, []ElicitationQuestion{{Question: "Which bug?"}}); err != nil {
		t.Fatalf("record questions: %v", err)
	}
	blocked, _, err := store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + "Which bug?",
		Description: "clarify",
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	ready, err := store.UpdateTaskState(ctx, blocked.ID, blocked.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("unblock parent to ready: %v", err)
	}
	running, err = store.MarkTaskRunning(ctx, ready.ID, ready.UpdatedAt, 42)
	if err != nil {
		t.Fatalf("re-claim parent: %v", err)
	}

	project, err := store.GetProject(ctx, running.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	w := &Worker{store: store}
	_, blockedOut, err := w.runPreTaskElicitation(ctx, *running, *project)
	if err != nil {
		t.Fatalf("runPreTaskElicitation: %v", err)
	}
	if !blockedOut {
		t.Fatal("expected blocked=true when pending clarification child exists")
	}
	got, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", got.State)
	}
	if got.OSProcessID != nil {
		t.Fatalf("os_process_id = %v, want nil after re-block", got.OSProcessID)
	}
}

func TestReblockTaskForPendingHITL_AlreadyBlockedAfterConflict(t *testing.T) {
	t.Parallel()
	base := testutil.NewFakeStore()
	store := &transitionEnforcingStore{FakeKanbanStore: base}
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "reblock-race",
		Tasks:       []models.DraftTask{{Title: "Fix", Description: "fix the bug"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	parent := tasks[0]
	running, err := store.MarkTaskRunning(ctx, parent.ID, parent.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	stale := *running
	if _, _, err := store.BlockTaskWithSubtasks(ctx, running.ID, running.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + "Which bug?",
		Description: "clarify",
		Assignee:    models.TaskAssigneeHuman,
	}}); err != nil {
		t.Fatalf("block: %v", err)
	}

	w := &Worker{store: store}
	got, err := w.reblockTaskForPendingHITL(ctx, stale)
	if err != nil {
		t.Fatalf("reblockTaskForPendingHITL: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED", got.State)
	}
	if got.OSProcessID != nil {
		t.Fatalf("os_process_id = %v, want nil", got.OSProcessID)
	}
}

func TestFormatClarificationsBlock(t *testing.T) {
	t.Parallel()
	questions := []ElicitationQuestion{{Question: "Which service?"}}
	block := formatClarificationsBlock(questions, "payments API")
	if !strings.Contains(block, clarificationsBlockHeader) {
		t.Fatalf("block = %q", block)
	}
	if !strings.Contains(block, "payments API") {
		t.Fatalf("block = %q", block)
	}
}
