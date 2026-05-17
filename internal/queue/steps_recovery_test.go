package queue

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
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
	s.gateway.err = nil
	s.gateway.content = `{"command":"false"}`
	s.sandbox.result = sandbox.Result{Success: false, ExitCode: 1, Stderr: "boom"}
	task, ok := s.store.first(models.TaskStateReady)
	if !ok {
		return fmt.Errorf("ready task not found")
	}
	queued, err := s.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
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

func (s *queueScenario) daemonIsRunning(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan error, 1)
	go func() { s.done <- s.daemon.Start(s.ctx) }()
	return nil
}

func (s *queueScenario) workerRunningSandbox(ctx context.Context) error {
	s.store.seed(1, models.TaskStateReady)
	s.sandbox = &queueSandbox{blockOnCtx: true, started: make(chan struct{}), cancelled: make(chan struct{})}
	s.rebuild(1)
	go func() { _, _, _ = s.daemon.dispatch(s.ctx) }()
	select {
	case <-s.sandbox.started:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("sandbox did not start")
	case <-ctx.Done():
		return ctx.Err()
	}
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
	case <-s.sandbox.cancelled:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("sandbox did not observe cancellation")
	}
}

func (s *queueScenario) appExitsAfterRelease(context.Context) error {
	select {
	case err := <-s.done:
		if err != nil {
			return err
		}
	case <-time.After(time.Second):
		return fmt.Errorf("daemon did not stop")
	}
	return requireEqual("in-use slots", s.daemon.sem.InUse(), 0)
}
