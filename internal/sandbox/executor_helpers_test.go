package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/bus"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type recordingSink struct {
	mu     sync.Mutex
	events []models.Event
}

func (s *recordingSink) Emit(_ context.Context, evt models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

var sleepSeq atomic.Int64

// uniqueSleep returns a long sleep command no other process shares, and the
// pgrep pattern that finds only it. pgrep -f reads every process on the
// machine, so a fixed "sleep 300" also matches the same test running in
// another process (a parallel `make check`, a second worktree) while that
// copy's sleep is still inside its own timeout (B-019).
func uniqueSleep() (command, pattern string) {
	marker := fmt.Sprintf("%d%03d", os.Getpid(), sleepSeq.Add(1))
	return "sleep 300." + marker, "sleep 300\\." + marker
}

// assertProcessGone waits, bounded, for no process to match pattern. Signal
// delivery and reaping are asynchronous, so one sample taken right after the
// kill measures scheduler latency rather than whether the kill happened.
func assertProcessGone(t *testing.T, pattern string) {
	t.Helper()
	const budget = 5 * time.Second
	deadline := time.Now().Add(budget)
	for {
		out, err := exec.Command("pgrep", "-f", pattern).CombinedOutput()
		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q still running %s after the kill: %s", pattern, budget, out)
		}
		time.Sleep(20 * time.Millisecond)
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
