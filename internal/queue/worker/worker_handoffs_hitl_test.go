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
	store := &reviewMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "review-test",
		Tasks: []models.DraftTask{{Title: "task-review-1", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(context.Background(), task, "Here is my draft output")

	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks to be called")
	}
	if len(store.subtasks) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(store.subtasks))
	}
	sub := store.subtasks[0]
	if sub.Assignee != models.TaskAssigneeHuman {
		t.Fatalf("subtask assignee = %q, want HUMAN", sub.Assignee)
	}
	if !strings.Contains(sub.Title, "Review required") {
		t.Fatalf("subtask title = %q, should contain 'Review required'", sub.Title)
	}
	if !strings.Contains(sub.Description, "Review required before task completion") {
		t.Fatalf("subtask description should contain structured header, got %q", sub.Description)
	}
	if !strings.Contains(sub.Description, "draft output") {
		t.Fatalf("subtask description should contain draft output reference")
	}

	found := false
	for _, ev := range sink.events {
		if ev.Type == "REVIEW_HANDOFF" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected REVIEW_HANDOFF event to be emitted")
	}
}

func TestCreateReviewHandoff_StoreError(t *testing.T) {
	t.Parallel()
	store := &reviewMockStore{FakeKanbanStore: testutil.NewFakeStore(), err: errMockBlock}
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "review-err",
		Tasks: []models.DraftTask{{Title: "task-review-err", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(context.Background(), task, "draft")

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
	subtasks    []models.DraftTask
	err         error
}

func (s *reviewMockStore) AddComment(ctx context.Context, c models.Comment) error {
	return s.FakeKanbanStore.AddComment(ctx, c)
}

func (s *reviewMockStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	s.blockCalled = true
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
