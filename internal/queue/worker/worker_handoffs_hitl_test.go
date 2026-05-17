package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// --- FormatForHuman ---

func TestFormatForHuman_AllFields(t *testing.T) {
	t.Parallel()
	msg := HITLMessage{
		Summary: "Deploy approval needed",
		Action:  "Click approve or reject",
		Urgency: "blocking",
		Detail:  "Target: production\nService: api",
	}
	result := FormatForHuman(msg)
	if !strings.Contains(result, "## Summary") {
		t.Fatal("missing Summary header")
	}
	if !strings.Contains(result, "Deploy approval needed") {
		t.Fatal("missing summary text")
	}
	if !strings.Contains(result, "## Required Action") {
		t.Fatal("missing Required Action header")
	}
	if !strings.Contains(result, "Click approve or reject") {
		t.Fatal("missing action text")
	}
	if !strings.Contains(result, "## Urgency") {
		t.Fatal("missing Urgency header")
	}
	if !strings.Contains(result, "blocking") {
		t.Fatal("missing urgency text")
	}
	if !strings.Contains(result, "## Detail") {
		t.Fatal("missing Detail header")
	}
	if !strings.Contains(result, "Target: production") {
		t.Fatal("missing detail text")
	}
}

func TestFormatForHuman_NoDetail(t *testing.T) {
	t.Parallel()
	msg := HITLMessage{
		Summary: "Simple message",
		Action:  "Acknowledge",
		Urgency: "low",
	}
	result := FormatForHuman(msg)
	if strings.Contains(result, "## Detail") {
		t.Fatal("should not include Detail section when empty")
	}
	if !strings.Contains(result, "Simple message") {
		t.Fatal("missing summary")
	}
}

// --- createReviewHandoff ---

func TestCreateReviewHandoff_CreatesSubtask(t *testing.T) {
	t.Parallel()
	store, sink, w, task := setupReviewHandoffFixture(t)
	const draft = "Here is my draft output"
	w.createReviewHandoff(context.Background(), task, draft)
	assertReviewHandoffCreated(t, store, sink, task, draft)
}

func setupReviewHandoffFixture(t *testing.T) (*reviewMockStore, *mockEventSink, *Worker, models.Task) {
	t.Helper()
	store := &reviewMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "review-test",
		Tasks:       []models.DraftTask{{Title: "task-review-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	return store, sink, w, tasks[0]
}

func assertReviewHandoffCreated(t *testing.T, store *reviewMockStore, sink *mockEventSink, task models.Task, draft string) {
	t.Helper()
	assertReviewBlockTask(t, store, task)
	assertReviewSubtask(t, store.subtasks[0])
	assertReviewDraftComment(t, store, task.ID, draft)
	assertReviewHandoffEvent(t, sink)
}

func assertReviewBlockTask(t *testing.T, store *reviewMockStore, task models.Task) {
	t.Helper()
	if store.blockTaskID != task.ID || !store.blockAt.Equal(task.UpdatedAt) || !store.blockCalled {
		t.Fatalf("block mismatch: id=%q at=%s called=%v", store.blockTaskID, store.blockAt, store.blockCalled)
	}
	if len(store.subtasks) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(store.subtasks))
	}
}

func assertReviewSubtask(t *testing.T, sub models.DraftTask) {
	t.Helper()
	if sub.Assignee != models.TaskAssigneeHuman {
		t.Fatalf("subtask assignee = %q, want HUMAN", sub.Assignee)
	}
	for _, want := range []string{"Review required", "Review required before task completion", "draft output"} {
		if !strings.Contains(sub.Title+sub.Description, want) {
			t.Fatalf("subtask missing %q: title=%q desc=%q", want, sub.Title, sub.Description)
		}
	}
}

func assertReviewDraftComment(t *testing.T, store *reviewMockStore, taskID, draft string) {
	t.Helper()
	comments, err := store.ListComments(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	for _, c := range comments {
		if strings.HasPrefix(c.Body, hitlDraftReviewCommentPrefix) && strings.Contains(c.Body, draft) {
			return
		}
	}
	t.Fatal("expected draft review comment on parent task")
}

func assertReviewHandoffEvent(t *testing.T, sink *mockEventSink) {
	t.Helper()
	for _, ev := range sink.events {
		if ev.Type == "REVIEW_HANDOFF" {
			return
		}
	}
	t.Fatal("expected REVIEW_HANDOFF event to be emitted")
}

func TestCreateReviewHandoff_RefreshesTaskUpdatedAt(t *testing.T) {
	t.Parallel()
	store := &reviewRefreshedMockStore{reviewMockStore: reviewMockStore{FakeKanbanStore: testutil.NewFakeStore()}}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{})

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "review-refresh",
		Tasks:       []models.DraftTask{{Title: "task-review-refresh", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	store.freshUpdatedAt = parent.UpdatedAt.Add(time.Hour)

	staleTask := parent
	staleTask.UpdatedAt = parent.UpdatedAt.Add(-time.Hour)

	w.createReviewHandoff(context.Background(), staleTask, "draft output")

	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks to be called")
	}
	if !store.blockAt.Equal(store.freshUpdatedAt) {
		t.Fatalf("BlockTaskWithSubtasks updatedAt = %v, want refreshed %v", store.blockAt, store.freshUpdatedAt)
	}
}

func TestCreateReviewHandoff_StoreError(t *testing.T) {
	t.Parallel()
	store := &reviewMockStore{FakeKanbanStore: testutil.NewFakeStore(), err: errMockBlock}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "review-err",
		Tasks:       []models.DraftTask{{Title: "task-review-err", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(context.Background(), task, "draft")

	if store.blockTaskID != task.ID {
		t.Fatalf("BlockTaskWithSubtasks taskID = %q, want %q", store.blockTaskID, task.ID)
	}
	if !store.blockAt.Equal(task.UpdatedAt) {
		t.Fatalf("BlockTaskWithSubtasks updatedAt = %s, want %s", store.blockAt, task.UpdatedAt)
	}

	found := false
	for _, ev := range sink.events {
		if ev.Type == "ERROR" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ERROR event when store fails")
	}
}

// --- reviewMockStore ---

var errMockBlock = models.ErrStateConflict

type reviewMockStore struct {
	*testutil.FakeKanbanStore
	blockCalled bool
	blockTaskID string
	blockAt     time.Time
	subtasks    []models.DraftTask
	err         error
}

func (s *reviewMockStore) AddComment(ctx context.Context, c models.Comment) error {
	return s.FakeKanbanStore.AddComment(ctx, c)
}

type reviewRefreshedMockStore struct {
	reviewMockStore
	freshUpdatedAt time.Time
}

func (s *reviewRefreshedMockStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	task, err := s.FakeKanbanStore.GetTask(ctx, id)
	if err != nil || task == nil {
		return task, err
	}
	task.UpdatedAt = s.freshUpdatedAt
	return task, nil
}

func (s *reviewMockStore) BlockTaskWithSubtasks(_ context.Context, taskID string, at time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	s.blockCalled = true
	s.blockTaskID = taskID
	s.blockAt = at
	s.subtasks = subtasks
	if s.err != nil {
		return nil, nil, s.err
	}
	created := make([]models.Task, len(subtasks))
	for i, d := range subtasks {
		created[i] = models.Task{
			BaseEntity: models.BaseEntity{ID: "sub-" + d.Title},
			Title:      d.Title,
			Assignee:   d.Assignee,
		}
	}
	return &models.Task{}, created, nil
}
