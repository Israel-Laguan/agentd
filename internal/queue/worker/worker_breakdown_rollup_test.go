package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

const breakdownMarkerTitle = "AGENT_PLAN"

// titleBreakdownGateway decomposes any task whose prompt carries
// breakdownMarkerTitle and returns a shell command otherwise, like a model that always splits
// plan-marked tasks.
type titleBreakdownGateway struct {
	routingTestGateway
}

func (g *titleBreakdownGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, breakdownMarkerTitle) {
			return gateway.AIResponse{Content: `{"too_complex":true,"subtasks":[` +
				`{"title":"Step 1","description":"scaffold"},{"title":"Step 2","description":"implement"}]}`}, nil
		}
	}
	return gateway.AIResponse{Content: `{"command":"echo ok"}`}, nil
}

func newBreakdownRollupFixture(t *testing.T) (*testutil.FakeKanbanStore, *titleBreakdownGateway, *Worker, models.Task) {
	t.Helper()
	store := testutil.NewFakeStore()
	if err := store.UpsertAgentProfile(context.Background(), models.AgentProfile{ID: "default", Provider: "ollama", Model: "llama3"}); err != nil {
		t.Fatalf("UpsertAgentProfile: %v", err)
	}
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "breakdown-rollup",
		Tasks:       []models.DraftTask{{Title: breakdownMarkerTitle + ": Produce a status report", Description: "decompose me", AgentID: "default"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	gw := &titleBreakdownGateway{}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &mockEventSink{}, WorkerOptions{MaxToolIterations: 5})
	return store, gw, w, tasks[0]
}

func mustGetTask(t *testing.T, store *testutil.FakeKanbanStore, id string) models.Task {
	t.Helper()
	task, err := store.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", id, err)
	}
	return *task
}

func mustListChildren(t *testing.T, store *testutil.FakeKanbanStore, id string) []models.Task {
	t.Helper()
	children, err := store.ListChildTasks(context.Background(), id)
	if err != nil {
		t.Fatalf("ListChildTasks(%s): %v", id, err)
	}
	return children
}

// A decomposed parent that resumes after its subtasks complete must be rolled
// up to COMPLETED, not re-prompted: re-prompting the identical task made the
// model decompose it again on every resume.
func TestBreakdownRollup_ResumedParentCompletesWithoutRedecomposing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, gw, w, parent := newBreakdownRollupFixture(t)

	w.Process(ctx, parent)
	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateBlocked {
		t.Fatalf("parent state after breakdown = %s, want BLOCKED", got)
	}
	children := mustListChildren(t, store, parent.ID)
	if len(children) != 2 {
		t.Fatalf("children after breakdown = %d, want 2", len(children))
	}

	for _, child := range children {
		w.Process(ctx, mustGetTask(t, store, child.ID))
	}
	resumed := mustGetTask(t, store, parent.ID)
	if resumed.State != models.TaskStateReady {
		t.Fatalf("parent state after children completed = %s, want READY", resumed.State)
	}

	requestsBefore := len(gw.requests)
	w.Process(ctx, resumed)

	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("resumed parent state = %s, want COMPLETED", got)
	}
	if got := len(mustListChildren(t, store, parent.ID)); got != 2 {
		t.Fatalf("children after resume = %d, want 2 (parent was decomposed again)", got)
	}
	if got := len(gw.requests) - requestsBefore; got != 0 {
		t.Fatalf("resumed parent made %d gateway requests, want 0", got)
	}
}

// The batch dispatch path rolls a resumed parent up too, instead of sending
// it to the model alongside the other claimed tasks.
func TestBreakdownRollup_BatchPathCompletesResumedParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, gw, w, parent := newBreakdownRollupFixture(t)

	w.Process(ctx, parent)
	children := mustListChildren(t, store, parent.ID)
	w.Process(ctx, mustGetTask(t, store, children[0].ID))
	w.Process(ctx, mustGetTask(t, store, children[1].ID))

	_, others, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "batch-partner",
		Tasks:       []models.DraftTask{{Title: "Generate a greeting script", AgentID: "default"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}

	requestsBefore := len(gw.requests)
	w.ProcessBatch(ctx, []models.Task{mustGetTask(t, store, parent.ID), others[0]})

	if got := mustGetTask(t, store, others[0].ID).State; got != models.TaskStateCompleted {
		t.Fatalf("batch partner state = %s, want COMPLETED", got)
	}
	if got := mustGetTask(t, store, parent.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("resumed parent state = %s, want COMPLETED", got)
	}
	if got := len(mustListChildren(t, store, parent.ID)); got != 2 {
		t.Fatalf("children after batch resume = %d, want 2", got)
	}
	for _, req := range gw.requests[requestsBefore:] {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, breakdownMarkerTitle) {
				t.Fatal("resumed parent was sent to the model in the batch")
			}
		}
	}
}

// A parent resumed before its breakdown subtasks completed (e.g. a manual
// retry) is not rolled up; it runs normally.
func TestBreakdownRollup_IncompleteSubtasksFallThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, gw, w, parent := newBreakdownRollupFixture(t)

	w.Process(ctx, parent)
	blocked := mustGetTask(t, store, parent.ID)
	retried, err := store.UpdateTaskState(ctx, blocked.ID, blocked.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState(READY): %v", err)
	}

	requestsBefore := len(gw.requests)
	w.Process(ctx, *retried)

	if got := len(gw.requests) - requestsBefore; got != 1 {
		t.Fatalf("gateway requests on retry = %d, want 1", got)
	}
	if got := mustGetTask(t, store, parent.ID).State; got == models.TaskStateCompleted {
		t.Fatal("parent rolled up to COMPLETED while subtasks were still open")
	}
}

func TestLatestBreakdownSubtaskIDs_UsesNewestMarker(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	comments := []models.Comment{
		{BaseEntity: models.BaseEntity{CreatedAt: t0.Add(time.Second)}, Body: breakdownSubtasksPrefix + "c,d"},
		{BaseEntity: models.BaseEntity{CreatedAt: t0}, Body: breakdownSubtasksPrefix + "a,b"},
		{BaseEntity: models.BaseEntity{CreatedAt: t0.Add(2 * time.Second)}, Body: "unrelated"},
	}
	got := latestBreakdownSubtaskIDs(comments)
	if strings.Join(got, ",") != "c,d" {
		t.Fatalf("latestBreakdownSubtaskIDs = %v, want [c d]", got)
	}
	if ids := latestBreakdownSubtaskIDs([]models.Comment{{Body: "plain"}}); ids != nil {
		t.Fatalf("no marker: got %v, want nil", ids)
	}
}
