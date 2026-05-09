package sandbox

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"agentd/internal/bus"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type recordingSink struct {
	events []models.Event
}

func (s *recordingSink) Emit(_ context.Context, evt models.Event) error {
	s.events = append(s.events, evt)
	return nil
}

type persistingBusSink struct {
	store models.KanbanStore
	bus   bus.Bus
}

func (s *persistingBusSink) Emit(ctx context.Context, evt models.Event) error {
	if err := s.store.AppendEvent(ctx, evt); err != nil {
		return err
	}
	s.bus.Publish(ctx, bus.Signal{
		Topic:   "project:" + evt.ProjectID,
		Type:    string(evt.Type),
		Payload: evt.Payload,
	})
	return nil
}

func assertNoSleep300(t *testing.T) {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", "sleep 300").CombinedOutput()
	if err == nil {
		t.Fatalf("sleep 300 still exists: %s", out)
	}
}

func materializeSandboxTask(t *testing.T, store *testutil.FakeKanbanStore) (*models.Project, []models.Task) {
	t.Helper()
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "sandbox",
		Description: "sandbox test",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	return project, tasks
}

func receiveEvent(t *testing.T, ch <-chan bus.Signal, timeout time.Duration) bus.Signal {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event")
	}
	return bus.Signal{}
}

func assertEventCount(t *testing.T, store *testutil.FakeKanbanStore, want int) {
	t.Helper()
	got := 0
	for _, e := range store.Events() {
		if e.Type == "LOG_CHUNK" {
			got++
		}
	}
	if got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
