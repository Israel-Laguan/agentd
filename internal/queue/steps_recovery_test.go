package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
)

// --- Queue scenario steps (retries, ghost recovery, graceful shutdown) ---

func (s *queueScenario) readyTaskWithRetries(_ context.Context, retries int) error {
	s.store.seed(1, models.TaskStateReady)
	return s.store.updateFirst(func(task *models.Task) {
		task.RetryCount = retries
	})
}

func (s *queueScenario) maxRetryLimit(_ context.Context, limit int) error {
	s.maxRetries = limit
	s.rebuild(1)
	return nil
}

func (s *queueScenario) workerSeesSandboxFailure(ctx context.Context) error {
	// The failing command is the agent's now: a turn that fails is the task's
	// failure, and the retry ladder is what the coordinator still owns.
	s.useACP = true
	s.acpGateway = &fakeGateway{}
	s.rebuild(1)
	task, ok := s.store.first(models.TaskStateReady)
	if !ok {
		return fmt.Errorf("ready task not found")
	}
	updated, err := s.store.update(task.ID, func(task *models.Task) {
		task.Title = fakeagent.Script(fakeagent.Step{Error: "command failed"})
	})
	if err != nil {
		return err
	}
	queued, err := s.store.UpdateTaskState(ctx, updated.ID, updated.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		return err
	}
	s.worker.Process(ctx, *queued)
	return nil
}

func (s *queueScenario) taskRetryCountShouldBe(_ context.Context, want int) error {
	task, ok := s.store.first(models.TaskStateFailedRequiresHuman)
	if !ok {
		return fmt.Errorf("failed task not found")
	}
	return requireEqual("retry count", task.RetryCount, want)
}

func (s *queueScenario) taskShouldBeFailedRequiresHuman(context.Context) error {
	return requireEqual("FAILED_REQUIRES_HUMAN tasks", s.store.count(models.TaskStateFailedRequiresHuman), 1)
}

func (s *queueScenario) poisonPillHandoffEmitted(context.Context) error {
	if s.sink.containsType("POISON_PILL_HANDOFF") {
		return nil
	}
	return fmt.Errorf("POISON_PILL_HANDOFF event not found")
}

func (s *queueScenario) failedTaskNotPicked(ctx context.Context) error {
	return s.nextTickIgnores(ctx, 0)
}

func (s *queueScenario) runningTaskWithPID(_ context.Context, pid int) error {
	s.store.seed(1, models.TaskStateRunning)
	return s.store.updateFirst(func(task *models.Task) {
		task.OSProcessID = &pid
	})
}

func (s *queueScenario) pidDoesNotExist(_ context.Context, pid int) error {
	s.probe = StaticPIDProbe{PIDs: nil}
	s.rebuild(1)
	return nil
}

func (s *queueScenario) pidIsAlive(_ context.Context, pid int) error {
	s.probe = StaticPIDProbe{PIDs: []int{pid}}
	s.rebuild(1)
	return nil
}

func (s *queueScenario) bootSequence(ctx context.Context) error {
	return BootReconcile(ctx, s.store, s.probe, s.sink)
}

func (s *queueScenario) ghostDetected(context.Context) error {
	return requireEqual("ready tasks", s.store.count(models.TaskStateReady), 1)
}

func (s *queueScenario) taskShouldBeReady(context.Context) error {
	return requireEqual("ready tasks", s.store.count(models.TaskStateReady), 1)
}

func (s *queueScenario) pidShouldBeNull(context.Context) error {
	task, ok := s.store.first(models.TaskStateReady)
	if !ok || task.OSProcessID != nil {
		return fmt.Errorf("os_process_id = %#v, want nil", task.OSProcessID)
	}
	return nil
}

func (s *queueScenario) recoveryEventLogged(context.Context) error {
	if !s.sink.contains("Recovered Ghost Task") && !s.sink.contains("reset ghost task") {
		return fmt.Errorf("recovery event not found")
	}
	return nil
}

func (s *queueScenario) taskShouldRemainRunning(context.Context) error {
	return requireEqual("running tasks", s.store.count(models.TaskStateRunning), 1)
}

func (s *queueScenario) ghostNotModified(context.Context) error {
	task, ok := s.store.first(models.TaskStateRunning)
	if !ok || task.OSProcessID == nil || *task.OSProcessID != 1234 {
		return fmt.Errorf("task modified: %#v", task)
	}
	return nil
}

func (s *queueScenario) daemonIsRunning(context.Context) error {
	// Use a background-derived context so godog scenario cancellation does not
	// race with daemon shutdown (Start waits for in-flight workers on s.ctx).
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan error, 1)
	return nil
}

func (s *queueScenario) startDaemon() {
	if s.daemonStarted {
		return
	}
	s.daemonStarted = true
	go func() { s.done <- s.daemon.Start(s.ctx) }()
}

func (s *queueScenario) workerRunningSandbox(context.Context) error {
	s.store.seed(1, models.TaskStateReady)
	// The marker is the fake agent's Touch side effect, which fires when the
	// turn starts. Waiting on it (rather than the Command hook, which fires
	// before cmd.Start) ensures the interrupt cancels an in-flight turn.
	marker := fmt.Sprintf("agentd-ready-%d.marker", time.Now().UnixNano())
	markerPath := filepath.Join(s.store.project.WorkspacePath, marker)
	defer func() { _ = os.Remove(markerPath) }()
	for i := range s.store.tasks {
		s.store.tasks[i].Title = fakeagent.Script(
			fakeagent.Step{Touch: marker},
			fakeagent.Step{SleepMS: 60_000},
		)
	}
	s.useACP = true
	s.acpGateway = &fakeGateway{}
	s.acpStarted, s.acpStop = make(chan struct{}), make(chan struct{})
	s.acp, s.closeACP = newObservedACPRuntime(s.acpStarted, s.acpStop)
	s.rebuild(1)
	s.startDaemon()
	go func() { _, _, _ = s.daemon.dispatch(s.ctx) }()
	return waitFor(func() bool {
		_, err := os.Stat(markerPath)
		return err == nil
	}, "the agent to start its turn")
}

func (s *queueScenario) osInterrupt(context.Context) error {
	s.cancel()
	return nil
}

func (s *queueScenario) rootContextCancelled(context.Context) error {
	select {
	case <-s.ctx.Done():
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("root context was not cancelled")
	}
}

func (s *queueScenario) sandboxCaughtCancel(context.Context) error {
	select {
	case <-s.acpStop:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("the agent process did not observe cancellation")
	}
}

func (s *queueScenario) appExitsAfterRelease(context.Context) error {
	select {
	case err := <-s.done:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("daemon did not stop")
	}
	return waitFor(func() bool { return s.daemon.sem.InUse() == 0 }, "in-use slots")
}
