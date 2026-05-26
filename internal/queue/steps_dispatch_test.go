package queue

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// --- Queue scenario steps (dispatch, breaker, outage) ---

func (s *queueScenario) maxWorkersLimit(_ context.Context, limit int) error {
	s.sandbox = newBlockingQueueSandbox()
	s.rebuild(limit)
	return nil
}

func (s *queueScenario) readyTasks(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	return nil
}

func (s *queueScenario) daemonTicks(ctx context.Context) error {
	before := s.store.count(models.TaskStateQueued) + s.store.count(models.TaskStateRunning)
	if _, _, err := s.daemon.dispatch(ctx); err != nil {
		return err
	}
	s.lastQueued = s.store.count(models.TaskStateQueued) + s.store.count(models.TaskStateRunning) - before
	return nil
}

func (s *queueScenario) tasksShouldBeQueued(_ context.Context, want int) error {
	return waitFor(func() bool {
		return s.store.count(models.TaskStateQueued)+s.store.count(models.TaskStateRunning) == want
	}, "claimed tasks")
}

func (s *queueScenario) semaphoreAvailable(_ context.Context, want int) error {
	return waitFor(func() bool { return s.daemon.sem.Available() == want }, "available slots")
}

func (s *queueScenario) nextTickIgnores(ctx context.Context, _ int) error {
	before := s.store.count(models.TaskStateQueued) + s.store.count(models.TaskStateRunning)
	if _, _, err := s.daemon.dispatch(ctx); err != nil {
		return err
	}
	after := s.store.count(models.TaskStateQueued) + s.store.count(models.TaskStateRunning)
	return requireEqual("newly claimed tasks", after-before, 0)
}

func (s *queueScenario) oneWorkerFinishes(ctx context.Context) error {
	task, ok := s.store.first(models.TaskStateRunning)
	if !ok {
		task, ok = s.store.first(models.TaskStateQueued)
	}
	if !ok {
		return fmt.Errorf("no active task found")
	}
	if _, err := s.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{Success: true}); err != nil {
		return err
	}
	s.daemon.sem.Release()
	return nil
}

func (s *queueScenario) nextTickPicks(ctx context.Context, want int) error {
	if err := s.daemonTicks(ctx); err != nil {
		return err
	}
	return requireEqual("newly claimed tasks", s.lastQueued, want)
}

func (s *queueScenario) breakerClosed(context.Context) error {
	s.breaker.RecordSuccess()
	return nil
}

func (s *queueScenario) gatewayUnreachable(context.Context) error {
	s.gateway.err = models.ErrLLMUnreachable
	s.rebuild(3)
	return nil
}

func (s *queueScenario) threeWorkersFailOutage(ctx context.Context) error {
	s.store.seed(3, models.TaskStateReady)
	_, _, err := s.daemon.dispatch(ctx)
	if err != nil {
		return err
	}
	return waitFor(func() bool { return s.daemon.sem.InUse() == 0 }, "outage workers finished")
}

func (s *queueScenario) breakerShouldBeOpen(context.Context) error {
	return waitFor(func() bool { return s.breaker.State() == BreakerOpen }, "breaker OPEN")
}

func (s *queueScenario) outageTasksReadyThenHumanHandoff(context.Context) error {
	return waitFor(func() bool { return s.outageHandoffSettled() }, "outage handoff settlement")
}

func (s *queueScenario) outageHandoffSettled() bool {
	tasks, _ := s.store.ListTasksByProject(context.Background(), "project")
	ready := 0
	blocked := 0
	for _, task := range tasks {
		if task.RetryCount != 0 {
			return false
		}
		switch task.State {
		case models.TaskStateReady:
			ready++
		case models.TaskStateBlocked:
			blocked++
		default:
			return false
		}
	}
	return ready == 2 && blocked == 1 && s.sink.containsType("PROVIDER_EXHAUSTED_HANDOFF")
}

func (s *queueScenario) daemonPausesPolling(ctx context.Context) error {
	s.store.seed(1, models.TaskStateReady)
	return s.nextTickIgnores(ctx, 1)
}

func (s *queueScenario) breakerOpen(context.Context) error {
	s.breaker.ForceStateForTest(BreakerOpen, s.now)
	return nil
}

func (s *queueScenario) breakerTimeoutElapsed(context.Context) error {
	s.now = s.now.Add(DefaultBreakerTimeout + time.Second)
	s.store.seed(3, models.TaskStateReady)
	s.sandbox = newBlockingQueueSandbox()
	s.rebuild(3)
	return nil
}

func (s *queueScenario) breakerShouldBeHalfOpen(context.Context) error {
	return requireBreakerState(s.breaker, BreakerHalfOpen)
}

func (s *queueScenario) oneProbeTask(context.Context) error {
	return waitFor(func() bool {
		active := s.store.count(models.TaskStateRunning) + s.store.count(models.TaskStateQueued)
		return active == 1
	}, "exactly one probe task")
}

func (s *queueScenario) testTaskSucceeds(_ context.Context) error {
	if err := waitFor(func() bool {
		return s.store.count(models.TaskStateRunning) == 1
	}, "probe task running"); err != nil {
		return err
	}
	if s.sandbox != nil {
		s.sandbox.unblockProbe()
	}
	return waitFor(func() bool {
		return s.store.count(models.TaskStateCompleted) >= 1 && s.daemon.sem.InUse() == 0
	}, "probe task completed")
}

func (s *queueScenario) breakerShouldBeClosed(context.Context) error {
	return requireBreakerState(s.breaker, BreakerClosed)
}

func (s *queueScenario) normalPollingResumes(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.daemon.sem.InUse() != 0 || s.store.count(models.TaskStateReady) != 2 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err := s.daemonTicks(ctx); err != nil {
			return err
		}
		if s.lastQueued == 2 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("newly claimed tasks = %d, want 2", s.lastQueued)
}

func newBlockingQueueSandbox() *queueSandbox {
	return &queueSandbox{
		result:     sandbox.Result{Success: true, ExitCode: 0},
		blockOnCtx: true,
		started:    make(chan struct{}),
		cancelled:  make(chan struct{}),
		unblock:    make(chan struct{}),
	}
}

func requireEqual(label string, got, want int) error {
	if got != want {
		return fmt.Errorf("%s = %d, want %d", label, got, want)
	}
	return nil
}

func requireBreakerState(b *CircuitBreaker, want BreakerState) error {
	if got := b.State(); got != want {
		return fmt.Errorf("breaker state = %s, want %s", got, want)
	}
	return nil
}

func waitFor(ok func() bool, label string) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", label)
}
