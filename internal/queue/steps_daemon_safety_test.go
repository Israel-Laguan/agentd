package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// --- Daemon safety steps (deadline, backoff, panic) ---

type daemonSafetyScenario struct {
	store   *queueStore
	breaker *CircuitBreaker
	gateway gateway.AIGateway
	worker  *Worker
	daemon  *Daemon
	sink    *queueSink

	delays      []time.Duration
	curDelay    time.Duration
	dispatchErr error

	// The deadline and panic scenarios run a task on the fake agent: after T-070
	// the only thing a dispatched task runs is an agent, so the reaper has an
	// agent process to cancel and a panic has to come from the agent's runtime.
	acp        *ACPRuntime
	closeACP   func()
	acpStarted chan struct{}
	acpStop    chan struct{}
	panicAgent bool
	useACP     bool
}

func newDaemonSafetyScenario() *daemonSafetyScenario {
	return &daemonSafetyScenario{}
}

func registerDaemonSafetySteps(sc *godog.ScenarioContext, s *daemonSafetyScenario) {
	// Task deadline / reaper
	sc.Step(`^the task deadline is set to (\S+)$`, s.setTaskDeadline)
	sc.Step(`^(\d+) READY task exists with a blocking sandbox$`, s.readyBlockingSandbox)
	sc.Step(`^(\d+) READY task exists with a fast sandbox$`, s.readyFastSandbox)
	sc.Step(`^the Daemon dispatches the task$`, s.dispatchOnce)
	sc.Step(`^the sandbox should start executing$`, s.sandboxStarted)
	sc.Step(`^the sandbox should be cancelled within the deadline$`, s.sandboxCancelled)
	sc.Step(`^the semaphore slot should be released$`, s.semaphoreReleased)
	sc.Step(`^the task should be COMPLETED$`, s.taskCompleted)

	// Adaptive backoff
	sc.Step(`^the base poll interval is (\S+) and the ceiling is (\S+)$`, s.setPollIntervals)
	sc.Step(`^the Daemon dispatches and finds 0 tasks (\d+) times in a row$`, s.dispatchEmptyN)
	sc.Step(`^the polling intervals should be (\S+), (\S+), (\S+)$`, s.assertDelays3)
	sc.Step(`^the Daemon has backed off to (\S+)$`, s.backoffTo)
	sc.Step(`^the Daemon dispatches and claims (\d+) task$`, s.dispatchWithClaim)
	sc.Step(`^the polling interval should reset to the base (\S+)$`, s.assertDelayReset)
	sc.Step(`^the polling interval should be (\S+)$`, s.assertCurrentDelay)

	// Panic safety
	sc.Step(`^the Daemon has (\d+) worker slot$`, s.daemonWithSlots)
	sc.Step(`^the worker panics during task processing$`, s.workerPanics)
	sc.Step(`^the Daemon dispatches (\d+) READY task$`, s.dispatchNTasks)
	sc.Step(`^the semaphore slot should be released after the panic$`, s.semaphoreReleased)
	sc.Step(`^no unrecovered panic should propagate to the Daemon$`, s.noPanicPropagated)
}

func (s *daemonSafetyScenario) setTaskDeadline(_ context.Context, raw string) error {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	s.store = newQueueStore()
	s.breaker = NewCircuitBreaker()
	s.gateway = &fakeGateway{}
	s.sink = &queueSink{}
	s.useACP = true
	s.rebuildWorker()
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: 1, TaskInterval: time.Hour, TaskDeadline: d,
		Probe: StaticPIDProbe{},
	})
	return nil
}

// rebuildWorker builds the worker this scenario dispatches with. The agent's
// runtime signals when a prompt starts and when the reaper's cancellation kills
// the agent process.
func (s *daemonSafetyScenario) rebuildWorker() {
	if s.closeACP != nil {
		s.closeACP()
	}
	s.acpStarted, s.acpStop = make(chan struct{}), make(chan struct{})
	if s.panicAgent {
		s.acp, s.closeACP = newPanicACPRuntime(), func() {}
	} else {
		s.acp, s.closeACP = newObservedACPRuntime(s.acpStarted, s.acpStop)
	}
	opts := WorkerOptions{}
	if s.useACP {
		opts.Routes, opts.ACP = acpQueueRoutes(), s.acp
	}
	s.worker = NewWorker(s.store, s.gateway, nil, s.breaker, s.sink, opts)
}

// readyBlockingSandbox seeds a task whose agent stays in its turn until the
// reaper's deadline cancels it.
func (s *daemonSafetyScenario) readyBlockingSandbox(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	for i := range s.store.tasks {
		s.store.tasks[i].Title = fakeagent.Script(fakeagent.Step{SleepMS: 60_000})
	}
	return nil
}

func (s *daemonSafetyScenario) readyFastSandbox(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	for i := range s.store.tasks {
		s.store.tasks[i].Title = fakeagent.Script(acpReport("done"))
	}
	return nil
}

func (s *daemonSafetyScenario) dispatchOnce(context.Context) error {
	_, _, err := s.daemon.dispatch(context.Background())
	return err
}

func (s *daemonSafetyScenario) sandboxStarted(context.Context) error {
	select {
	case <-s.acpStarted:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("the agent did not start")
	}
}

func (s *daemonSafetyScenario) sandboxCancelled(context.Context) error {
	select {
	case <-s.acpStop:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("the agent process was not cancelled by the reaper")
	}
}

func (s *daemonSafetyScenario) semaphoreReleased(context.Context) error {
	deadline := time.After(5 * time.Second)
	for s.daemon.sem.Available() != s.daemon.sem.Capacity() {
		select {
		case <-deadline:
			return fmt.Errorf("semaphore not released (available=%d, capacity=%d)",
				s.daemon.sem.Available(), s.daemon.sem.Capacity())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}

func (s *daemonSafetyScenario) taskCompleted(context.Context) error {
	return waitFor(func() bool {
		return s.store.count(models.TaskStateCompleted) == 1
	}, "completed task")
}

func (s *daemonSafetyScenario) setPollIntervals(_ context.Context, baseRaw, ceilRaw string) error {
	base, err := time.ParseDuration(baseRaw)
	if err != nil {
		return err
	}
	ceil, err := time.ParseDuration(ceilRaw)
	if err != nil {
		return err
	}
	s.store = newQueueStore()
	s.breaker = NewCircuitBreaker()
	s.sink = &queueSink{}
	s.daemon = NewDaemon(s.store, nil, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: 1, TaskInterval: base, MaxTaskInterval: ceil,
		Probe: StaticPIDProbe{},
	})
	s.curDelay = base
	s.delays = nil
	return nil
}

func (s *daemonSafetyScenario) dispatchEmptyN(_ context.Context, n int) error {
	for range n {
		s.curDelay = s.daemon.nextDispatchDelay(s.curDelay, 0, 0)
		s.delays = append(s.delays, s.curDelay)
	}
	return nil
}

func (s *daemonSafetyScenario) assertDelays3(_ context.Context, a, b, c string) error {
	expected := []string{a, b, c}
	if len(s.delays) < 3 {
		return fmt.Errorf("only %d delays recorded", len(s.delays))
	}
	for i, raw := range expected {
		want, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		if s.delays[i] != want {
			return fmt.Errorf("delay[%d] = %s, want %s", i, s.delays[i], want)
		}
	}
	return nil
}

func (s *daemonSafetyScenario) backoffTo(_ context.Context, raw string) error {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	s.curDelay = d
	return nil
}

func (s *daemonSafetyScenario) dispatchWithClaim(_ context.Context, _ int) error {
	s.curDelay = s.daemon.nextDispatchDelay(s.curDelay, 1, 0)
	return nil
}

func (s *daemonSafetyScenario) assertDelayReset(_ context.Context, raw string) error {
	want, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	if s.curDelay != want {
		return fmt.Errorf("delay = %s, want %s", s.curDelay, want)
	}
	return nil
}

func (s *daemonSafetyScenario) assertCurrentDelay(_ context.Context, raw string) error {
	want, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	if s.curDelay != want {
		return fmt.Errorf("delay = %s, want %s", s.curDelay, want)
	}
	return nil
}

func (s *daemonSafetyScenario) daemonWithSlots(_ context.Context, slots int) error {
	s.store = newQueueStore()
	s.breaker = NewCircuitBreaker()
	s.gateway = &queueGateway{toolCalls: []gateway.ToolCall{testutil.BashToolCall("call_1", "true")}}
	s.sink = &queueSink{}
	s.useACP = true
	s.rebuildWorker()
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: slots, TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})
	return nil
}

func (s *daemonSafetyScenario) workerPanics(context.Context) error {
	s.panicAgent = true
	s.rebuildWorker()
	// The daemon holds the worker it was built with, so it is rebuilt too.
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: s.daemon.sem.Capacity(), TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})
	return nil
}

func (s *daemonSafetyScenario) dispatchNTasks(_ context.Context, n int) error {
	s.store.seed(n, models.TaskStateReady)
	_, _, err := s.daemon.dispatch(context.Background())
	s.dispatchErr = err
	if err != nil {
		return err
	}
	return waitFor(func() bool {
		return s.daemon.sem.Available() == s.daemon.sem.Capacity()
	}, "dispatch goroutine completion after panic")
}

func (s *daemonSafetyScenario) noPanicPropagated(context.Context) error {
	if s.dispatchErr != nil {
		return fmt.Errorf("dispatch returned error: %v", s.dispatchErr)
	}
	if !s.sink.containsType("PANIC") {
		return fmt.Errorf("expected PANIC event from recovered worker panic")
	}
	return nil
}
