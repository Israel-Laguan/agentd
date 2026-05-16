package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"

	"github.com/cucumber/godog"
)

// --- Queue scenario steps (dispatch, breaker, outage) ---

func (s *queueScenario) maxWorkersLimit(_ context.Context, limit int) error {
	s.sandbox = &queueSandbox{blockOnCtx: true, started: make(chan struct{}), cancelled: make(chan struct{})}
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
	return err
}

func (s *queueScenario) breakerShouldBeOpen(context.Context) error {
	return waitFor(func() bool { return s.breaker.State() == BreakerOpen }, "breaker OPEN")
}

func (s *queueScenario) outageTasksReadyThenHumanHandoff(context.Context) error {
	tasks, _ := s.store.ListTasksByProject(context.Background(), "project")
	ready := 0
	blocked := 0
	for _, task := range tasks {
		if task.RetryCount != 0 {
			return fmt.Errorf("task %s retries=%d", task.ID, task.RetryCount)
		}
		switch task.State {
		case models.TaskStateReady:
			ready++
		case models.TaskStateBlocked:
			blocked++
		default:
			return fmt.Errorf("task %s state=%s", task.ID, task.State)
		}
	}
	if ready != 2 || blocked != 1 {
		return fmt.Errorf("ready=%d blocked=%d, want ready=2 blocked=1", ready, blocked)
	}
	if !s.sink.containsType("PROVIDER_EXHAUSTED_HANDOFF") {
		return fmt.Errorf("missing PROVIDER_EXHAUSTED_HANDOFF event")
	}
	return nil
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
	s.sandbox = &queueSandbox{blockOnCtx: true, started: make(chan struct{}), cancelled: make(chan struct{})}
	s.rebuild(3)
	return nil
}

func (s *queueScenario) breakerShouldBeHalfOpen(context.Context) error {
	return requireBreakerState(s.breaker, BreakerHalfOpen)
}

func (s *queueScenario) oneProbeTask(context.Context) error {
	return requireEqual("probe tasks", s.store.count(models.TaskStateRunning)+s.store.count(models.TaskStateQueued), 1)
}

func (s *queueScenario) testTaskSucceeds(ctx context.Context) error {
	task, ok := s.store.first(models.TaskStateQueued)
	if !ok {
		task, ok = s.store.first(models.TaskStateRunning)
	}
	if !ok {
		return fmt.Errorf("no probe task found")
	}
	if _, err := s.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{Success: true}); err != nil {
		return err
	}
	s.breaker.RecordSuccess()
	s.daemon.sem.Release()
	return nil
}

func (s *queueScenario) breakerShouldBeClosed(context.Context) error {
	return requireBreakerState(s.breaker, BreakerClosed)
}

func (s *queueScenario) normalPollingResumes(ctx context.Context) error {
	return s.nextTickPicks(ctx, 2)
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
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", label)
}

// --- Heartbeat reconciliation steps ---

func registerHeartbeatSteps(sc *godog.ScenarioContext, state *heartbeatScenario) {
	sc.Step(`^a running task with a heartbeat older than the stale threshold$`, state.runningTaskWithStaleHeartbeat)
	sc.Step(`^a running task with a recent heartbeat$`, state.runningTaskWithFreshHeartbeat)
	sc.Step(`^the OS PID for the task is not alive$`, state.pidNotAlive)
	sc.Step(`^the OS PID for the task is alive$`, state.pidAlive)
	sc.Step(`^the heartbeat reconciliation loop runs$`, state.reconcileHeartbeats)
	sc.Step(`^the task should be reset to READY$`, state.taskShouldBeReady)
	sc.Step(`^the task should remain RUNNING$`, state.taskShouldRemainRunning)
	sc.Step(`^a HEARTBEAT_RECONCILE event should be emitted$`, state.heartbeatEventEmitted)
	sc.Step(`^no HEARTBEAT_RECONCILE event should be emitted$`, state.noHeartbeatEvent)
}

type heartbeatScenario struct {
	store   *queueStore
	daemon  *Daemon
	probe   StaticPIDProbe
	sink    *queueSink
	taskPID int
}

func newHeartbeatScenario() *heartbeatScenario {
	s := &heartbeatScenario{}
	s.store = newQueueStore()
	s.sink = &queueSink{}
	s.probe = StaticPIDProbe{}
	s.taskPID = 12345
	s.daemon = NewDaemon(s.store, nil, nil, nil, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		Probe:          StaticPIDProbe{},
		StaleAfter:     2 * time.Minute,
	})
	return s
}

func (s *heartbeatScenario) rebuildDaemon() {
	s.daemon = NewDaemon(s.store, nil, nil, nil, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		Probe:          s.probe,
		StaleAfter:     2 * time.Minute,
	})
}

func (s *heartbeatScenario) seedRunning(heartbeat time.Time) {
	now := time.Now().UTC()
	pid := s.taskPID
	s.store.tasks = []models.Task{{
		BaseEntity:    models.BaseEntity{ID: "hb-task", CreatedAt: now, UpdatedAt: now},
		ProjectID:     "project",
		AgentID:       "default",
		Title:         "Heartbeat Test",
		State:         models.TaskStateRunning,
		Assignee:      models.TaskAssigneeSystem,
		OSProcessID:   &pid,
		LastHeartbeat: &heartbeat,
	}}
}

func (s *heartbeatScenario) runningTaskWithStaleHeartbeat(context.Context) error {
	s.seedRunning(time.Now().UTC().Add(-5 * time.Minute))
	return nil
}

func (s *heartbeatScenario) runningTaskWithFreshHeartbeat(context.Context) error {
	s.seedRunning(time.Now().UTC())
	return nil
}

func (s *heartbeatScenario) pidNotAlive(context.Context) error {
	s.probe = StaticPIDProbe{PIDs: nil}
	s.rebuildDaemon()
	return nil
}

func (s *heartbeatScenario) pidAlive(context.Context) error {
	s.probe = StaticPIDProbe{PIDs: []int{s.taskPID}}
	s.rebuildDaemon()
	return nil
}

func (s *heartbeatScenario) reconcileHeartbeats(context.Context) error {
	return s.daemon.reconcileHeartbeats(context.Background())
}

func (s *heartbeatScenario) taskShouldBeReady(context.Context) error {
	task, err := s.store.GetTask(context.Background(), "hb-task")
	if err != nil {
		return err
	}
	if task.State != models.TaskStateReady {
		return fmt.Errorf("task state = %s, want READY", task.State)
	}
	return nil
}

func (s *heartbeatScenario) taskShouldRemainRunning(context.Context) error {
	task, err := s.store.GetTask(context.Background(), "hb-task")
	if err != nil {
		return err
	}
	if task.State != models.TaskStateRunning {
		return fmt.Errorf("task state = %s, want RUNNING", task.State)
	}
	return nil
}

func (s *heartbeatScenario) heartbeatEventEmitted(context.Context) error {
	if !s.sink.containsType("HEARTBEAT_RECONCILE") {
		return fmt.Errorf("missing HEARTBEAT_RECONCILE event")
	}
	return nil
}

func (s *heartbeatScenario) noHeartbeatEvent(context.Context) error {
	if s.sink.containsType("HEARTBEAT_RECONCILE") {
		return fmt.Errorf("unexpected HEARTBEAT_RECONCILE event")
	}
	return nil
}

// --- Daemon safety steps (deadline, backoff, panic) ---

type daemonSafetyScenario struct {
	store   *queueStore
	breaker *CircuitBreaker
	gateway *queueGateway
	sb      *queueSandbox
	worker  *Worker
	daemon  *Daemon
	sink    *queueSink

	delays   []time.Duration
	curDelay time.Duration
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
	s.gateway = &queueGateway{content: `{"command":"true"}`}
	s.sink = &queueSink{}
	s.worker = NewWorker(s.store, s.gateway, nil, s.breaker, s.sink, WorkerOptions{})
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: 1, TaskInterval: time.Hour, TaskDeadline: d,
		Probe: StaticPIDProbe{},
	})
	return nil
}

func (s *daemonSafetyScenario) readyBlockingSandbox(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	s.sb = &queueSandbox{blockOnCtx: true, started: make(chan struct{}), cancelled: make(chan struct{})}
	s.worker.SetSandbox(s.sb)
	return nil
}

func (s *daemonSafetyScenario) readyFastSandbox(_ context.Context, count int) error {
	s.store.seed(count, models.TaskStateReady)
	s.sb = &queueSandbox{result: sandbox.Result{Success: true, ExitCode: 0}}
	s.worker.SetSandbox(s.sb)
	return nil
}

func (s *daemonSafetyScenario) dispatchOnce(context.Context) error {
	_, _, err := s.daemon.dispatch(context.Background())
	return err
}

func (s *daemonSafetyScenario) sandboxStarted(context.Context) error {
	select {
	case <-s.sb.started:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("sandbox did not start")
	}
}

func (s *daemonSafetyScenario) sandboxCancelled(context.Context) error {
	select {
	case <-s.sb.cancelled:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("sandbox was not cancelled by the reaper")
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
	s.gateway = &queueGateway{content: `{"command":"true"}`}
	s.sink = &queueSink{}
	s.worker = NewWorker(s.store, s.gateway, &queueSandbox{result: sandbox.Result{Success: true}}, s.breaker, s.sink, WorkerOptions{})
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: slots, TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})
	return nil
}

func (s *daemonSafetyScenario) workerPanics(context.Context) error {
	s.worker.SetSandbox(&panicSandbox{})
	return nil
}

func (s *daemonSafetyScenario) dispatchNTasks(_ context.Context, n int) error {
	s.store.seed(n, models.TaskStateReady)
	_, _, err := s.daemon.dispatch(context.Background())
	return err
}

func (s *daemonSafetyScenario) noPanicPropagated(context.Context) error {
	return nil
}

type panicSandbox struct {
	mu      sync.Mutex
	started bool
}

func (p *panicSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	p.mu.Lock()
	p.started = true
	p.mu.Unlock()
	panic("intentional test panic from sandbox")
}

// --- Resilience steps (outage handoff, disk watchdog) ---

func registerResilienceSteps(sc *godog.ScenarioContext, state *resilienceScenario) {
	// Outage handoff
	sc.Step(`^the circuit breaker has been open for longer than the handoff threshold$`, state.breakerOpenBeyondThreshold)
	sc.Step(`^the daemon checks for outage handoff$`, state.checkOutageHandoff)
	sc.Step(`^a HUMAN task titled "([^"]*)" should exist under the _system project$`, state.humanTaskExists)
	sc.Step(`^an LLM_OUTAGE_HANDOFF event should be emitted$`, state.outageHandoffEmitted)
	sc.Step(`^an outage HUMAN task already exists under the _system project$`, state.outageTaskAlreadyExists)
	sc.Step(`^the daemon checks for outage handoff again$`, state.checkOutageHandoff)
	sc.Step(`^no additional outage task should be created$`, state.noAdditionalOutageTask)
	sc.Step(`^the circuit breaker has been open for less than the handoff threshold$`, state.breakerOpenBelowThreshold)
	sc.Step(`^no outage task should be created$`, state.noOutageTask)

	// Disk watchdog
	sc.Step(`^the disk free percentage is below the configured threshold$`, state.diskBelowThreshold)
	sc.Step(`^the disk watchdog checks free space$`, state.checkDiskSpace)
	sc.Step(`^a DISK_SPACE_CRITICAL event should be emitted$`, state.diskCriticalEmitted)
	sc.Step(`^a disk alert HUMAN task already exists under the _system project$`, state.diskAlertAlreadyExists)
	sc.Step(`^the disk watchdog checks free space again$`, state.checkDiskSpace)
	sc.Step(`^no additional disk alert task should be created$`, state.noAdditionalDiskAlert)
	sc.Step(`^the disk free percentage is above the configured threshold$`, state.diskAboveThreshold)
	sc.Step(`^no disk alert task should be created$`, state.noDiskAlert)
}

type resilienceScenario struct {
	store          *resilienceStore
	breaker        *CircuitBreaker
	daemon         *Daemon
	sink           *queueSink
	systemTasksLen int
}

type resilienceStore struct {
	queueStore
	systemTasks []models.Task
}

func (s *resilienceStore) EnsureSystemProject(_ context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}

func (s *resilienceStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	for _, t := range s.systemTasks {
		if t.Title == draft.Title {
			return &t, false, nil
		}
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: fmt.Sprintf("sys-task-%d", len(s.systemTasks))},
		ProjectID:   projectID,
		Title:       draft.Title,
		Description: draft.Description,
		Assignee:    draft.Assignee,
	}
	s.systemTasks = append(s.systemTasks, task)
	return &task, true, nil
}

func newResilienceScenario() *resilienceScenario {
	s := &resilienceScenario{}
	s.store = &resilienceStore{queueStore: *newQueueStore()}
	s.breaker = NewCircuitBreaker()
	s.sink = &queueSink{}
	s.daemon = NewDaemon(s.store, nil, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		HandoffAfter:   2 * time.Minute,
	})
	return s
}

func (s *resilienceScenario) breakerOpenBeyondThreshold(context.Context) error {
	now := time.Now().UTC()
	s.breaker.ArmForResilienceTest(now, now.Add(-3*time.Minute), 5, models.ErrLLMUnreachable)
	return nil
}

func (s *resilienceScenario) checkOutageHandoff(context.Context) error {
	return s.daemon.checkOutageHandoff(context.Background())
}

func (s *resilienceScenario) humanTaskExists(_ context.Context, title string) error {
	for _, t := range s.store.systemTasks {
		if t.Title == title && t.Assignee == models.TaskAssigneeHuman {
			return nil
		}
	}
	return fmt.Errorf("no HUMAN task with title %q", title)
}

func (s *resilienceScenario) outageHandoffEmitted(context.Context) error {
	if !s.sink.containsType("LLM_OUTAGE_HANDOFF") {
		return fmt.Errorf("missing LLM_OUTAGE_HANDOFF event")
	}
	return nil
}

func (s *resilienceScenario) outageTaskAlreadyExists(context.Context) error {
	err := s.daemon.checkOutageHandoff(context.Background())
	if err != nil {
		return err
	}
	s.systemTasksLen = len(s.store.systemTasks)
	return nil
}

func (s *resilienceScenario) noAdditionalOutageTask(context.Context) error {
	if len(s.store.systemTasks) != s.systemTasksLen {
		return fmt.Errorf("tasks count changed from %d to %d", s.systemTasksLen, len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) breakerOpenBelowThreshold(context.Context) error {
	now := time.Now().UTC()
	s.breaker.ArmForResilienceTest(now, now.Add(-30*time.Second), 0, nil)
	return nil
}

func (s *resilienceScenario) noOutageTask(context.Context) error {
	if len(s.store.systemTasks) != 0 {
		return fmt.Errorf("unexpected %d tasks created", len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) diskBelowThreshold(context.Context) error {
	s.daemon.diskFreeThreshold = 10.0
	s.daemon.diskCheckPath = "/tmp"
	s.daemon.diskStat = func(_ string) (float64, error) { return 5.0, nil }
	return nil
}

func (s *resilienceScenario) checkDiskSpace(context.Context) error {
	return s.daemon.checkDiskSpace(context.Background())
}

func (s *resilienceScenario) diskCriticalEmitted(context.Context) error {
	if !s.sink.containsType("DISK_SPACE_CRITICAL") {
		return fmt.Errorf("missing DISK_SPACE_CRITICAL event")
	}
	return nil
}

func (s *resilienceScenario) diskAlertAlreadyExists(context.Context) error {
	err := s.daemon.checkDiskSpace(context.Background())
	if err != nil {
		return err
	}
	s.systemTasksLen = len(s.store.systemTasks)
	return nil
}

func (s *resilienceScenario) noAdditionalDiskAlert(context.Context) error {
	if len(s.store.systemTasks) != s.systemTasksLen {
		return fmt.Errorf("tasks count changed from %d to %d", s.systemTasksLen, len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) diskAboveThreshold(context.Context) error {
	s.daemon.diskFreeThreshold = 10.0
	s.daemon.diskCheckPath = "/tmp"
	s.daemon.diskStat = func(_ string) (float64, error) { return 50.0, nil }
	return nil
}

func (s *resilienceScenario) noDiskAlert(context.Context) error {
	if len(s.store.systemTasks) != 0 {
		return fmt.Errorf("unexpected %d tasks created", len(s.store.systemTasks))
	}
	return nil
}
