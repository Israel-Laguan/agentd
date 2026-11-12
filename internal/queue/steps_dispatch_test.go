package queue

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// --- Queue scenario steps (dispatch, breaker, outage) ---

func (s *queueScenario) maxWorkersLimit(_ context.Context, limit int) error {
	s.sandbox = newBlockingQueueSandbox()
	s.useACP = true
	// The task runs on an agent, and the pre-task clarify still makes an L1
	// call; give it a gateway that answers without clarifying.
	s.acpGateway = &fakeGateway{}
	s.rebuild(limit)
	return nil
}

func (s *queueScenario) readyTasks(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	if s.useACP {
		// A task holds its slot while its agent is in the turn, which is what
		// the concurrency cap has to hold down.
		for i := range s.store.tasks {
			s.store.tasks[i].Title = fakeagent.Script(fakeagent.Step{SleepMS: 60_000})
		}
	}
	return nil
}

func (s *queueScenario) daemonTicks(ctx context.Context) error {
	dispatched, _, err := s.daemon.dispatch(ctx)
	if err != nil {
		return err
	}
	s.lastQueued = dispatched
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
	dispatched, _, err := s.daemon.dispatch(ctx)
	if err != nil {
		return err
	}
	return requireEqual("newly claimed tasks", dispatched, 0)
}

func (s *queueScenario) oneWorkerFinishes(ctx context.Context) error {
	task, ok := s.store.first(models.TaskStateRunning)
	if !ok {
		task, ok = s.store.first(models.TaskStateQueued)
	}
	if !ok {
		return fmt.Errorf("no active task found")
	}
	// The task holds its slot while its agent is in the turn. Cancel the real
	// turn and wait for the worker to release the slot itself, so the next tick
	// cannot exceed the active-worker limit behind a still-running agent.
	if s.useACP {
		// The turn registers its cancel after MarkTaskRunning, so poll until a
		// RUNNING task has one.
		if err := waitFor(func() bool {
			running, ok := s.store.first(models.TaskStateRunning)
			return ok && s.canceller.Cancel(running.ID)
		}, "an in-flight agent turn to finish"); err != nil {
			return err
		}
		return waitFor(func() bool { return s.daemon.sem.Available() > 0 }, "the finished worker to release its slot")
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

func (s *queueScenario) breakerOpen(context.Context) error {
	s.breaker.ForceStateForTest(BreakerOpen, s.now)
	return nil
}

func (s *queueScenario) breakerTimeoutElapsed(context.Context) error {
	s.now = s.now.Add(DefaultBreakerTimeout + time.Second)
	s.store.seed(3, models.TaskStateReady)
	s.useACP = true
	s.acpGateway = &fakeGateway{}
	for i := range s.store.tasks {
		s.store.tasks[i].Title = fakeagent.Script(fakeagent.Step{Message: "checking"}, fakeagent.Step{SleepMS: 300}, acpReport("probe ok"))
	}
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
	return waitFor(func() bool {
		return s.store.count(models.TaskStateCompleted) >= 1 && s.daemon.sem.InUse() == 0
	}, "probe task completed")
}

func (s *queueScenario) breakerShouldBeClosed(context.Context) error {
	return requireBreakerState(s.breaker, BreakerClosed)
}

// An ACP task completes without a provider request, so it must not be read as
// recovery: the breaker stays HALF_OPEN and keeps admitting one probe at a time.
func (s *queueScenario) breakerShouldNotClose(context.Context) error {
	if state := s.breaker.State(); state == BreakerClosed {
		return fmt.Errorf("breaker state = %s, want it to stay HALF_OPEN without a provider request", state)
	}
	return nil
}

func (s *queueScenario) normalPollingResumes(ctx context.Context) error {
	// The tick is part of the poll on purpose: dispatch claims nothing until the
	// breaker lets it through, so the step has to keep ticking until it does. A
	// tick that claims tasks takes them out of READY, so the precondition below
	// stops it from ticking again.
	var tickErr error
	err := waitFor(func() bool {
		if s.daemon.sem.InUse() != 0 || s.store.count(models.TaskStateReady) != 2 {
			return false
		}
		if tickErr = s.daemonTicks(ctx); tickErr != nil {
			return true
		}
		return s.lastQueued == 2
	}, "normal polling to claim 2 tasks")
	if tickErr != nil {
		return tickErr
	}
	if err != nil {
		return fmt.Errorf("newly claimed tasks = %d, want 2: %w", s.lastQueued, err)
	}
	return nil
}

func newBlockingQueueSandbox() *queueSandbox {
	return &queueSandbox{
		result:     sandbox.Result{Success: true, ExitCode: 0},
		blockOnCtx: true,
		started:    make(chan struct{}),
		cancelled:  make(chan struct{}),
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

// stepWaitBudget bounds how long a scenario step polls for its condition.
//
// It was 3s, which B-009 recorded as flaky: under full-suite load the breaker
// scenario's three workers could not claim, run and record a failure inside 3s,
// so a timing artefact read as a product failure. The budget is now generous
// (30s) because every use of waitFor is a poll for an event that either happens
// on its own or never happens at all — there is no scenario that legitimately
// needs to *fail* fast, since a failing step returns an error the suite already
// surfaces. The old 3s was also an implicit default shared by every scenario;
// naming it here means a future step can raise it deliberately rather than by
// editing this function.
const stepWaitBudget = 30 * time.Second

// waitFor polls ok until it is true or the step budget expires. ok is called on
// a 10ms interval, so it should be a cheap, non-blocking check.
func waitFor(ok func() bool, label string) error {
	return waitForBudget(ok, label, stepWaitBudget)
}

// waitForBudget is waitFor with an explicit deadline, so a test can exercise the
// timeout path without spending the real budget.
func waitForBudget(ok func() bool, label string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ok() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s after %s", label, budget)
}
