package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestOutageHandoffCreatesHumanTaskWithDebugContext(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	breaker, now := openBreaker(t)
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, breaker, sink, DaemonOptions{MaxWorkers: 1, HandoffAfter: time.Minute})
	*now = (*now).Add(2 * time.Minute)

	if _, _, err := daemon.dispatch(ctx); err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "_system" {
		t.Fatalf("projects = %#v, want system project", projects)
	}
	tasks, err := store.ListTasksByProject(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("ListTasksByProject() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.Title != outageHandoffTitle || task.Assignee != models.TaskAssigneeHuman {
		t.Fatalf("task = %#v, want outage HUMAN task", task)
	}
	for _, want := range []string{"circuit breaker has been open", "Consecutive failures: 3", models.ErrLLMUnreachable.Error()} {
		if !strings.Contains(task.Description, want) {
			t.Fatalf("description missing %q:\n%s", want, task.Description)
		}
	}
	if len(sink.events) != 1 || sink.events[0].Type != "LLM_OUTAGE_HANDOFF" {
		t.Fatalf("events = %#v, want one LLM_OUTAGE_HANDOFF", sink.events)
	}
}

func TestOutageHandoffDeduplicatesOpenTask(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	breaker, now := openBreaker(t)
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, breaker, sink, DaemonOptions{MaxWorkers: 1, HandoffAfter: time.Minute})
	*now = (*now).Add(2 * time.Minute)

	if _, _, err := daemon.dispatch(ctx); err != nil {
		t.Fatalf("first dispatch() error = %v", err)
	}
	if _, _, err := daemon.dispatch(ctx); err != nil {
		t.Fatalf("second dispatch() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	tasks, err := store.ListTasksByProject(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("ListTasksByProject() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want deduplicated single task", len(tasks))
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want one handoff event", len(sink.events))
	}
}

func newQueueTestStore(_ *testing.T) *testutil.FakeKanbanStore {
	return testutil.NewFakeStore()
}

func openBreaker(t *testing.T) (*CircuitBreaker, *time.Time) {
	t.Helper()
	now := time.Now().UTC()
	breaker := NewCircuitBreaker()
	breaker.SetClockForTest(func() time.Time { return now })
	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", breaker.State())
	}
	return breaker, &now
}

type recordingSink struct {
	events []models.Event
}

func (s *recordingSink) Emit(_ context.Context, evt models.Event) error {
	s.events = append(s.events, evt)
	return nil
}
