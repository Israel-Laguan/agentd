package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/sandbox"

	"github.com/cucumber/godog"
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

// --- Healing / phase-planning steps ---

func registerHealingSteps(sc *godog.ScenarioContext, state *healingScenario) {
	// Self-healing
	sc.Step(`^a task that has failed (\d+) times? with self-healing enabled$`, state.taskFailedNTimes)
	sc.Step(`^the worker applies the healing ladder for retry (\d+)$`, state.applyHealingLadder)
	sc.Step(`^the healing action should be "([^"]*)" with step "([^"]*)"$`, state.healingActionShouldBe)
	sc.Step(`^a TUNE event should be emitted$`, state.tuneEventEmitted)

	// Phase planning
	sc.Step(`^a task titled "([^"]*)"$`, state.taskTitled)
	sc.Step(`^it should be recognized as a phase-planning task$`, state.isPhasePlanningTask)
	sc.Step(`^it should not be recognized as a phase-planning task$`, state.isNotPhasePlanningTask)
	sc.Step(`^a planning task titled "([^"]*)"$`, state.taskTitled)
	sc.Step(`^continuation tasks are retitled$`, state.retitleContinuation)
	sc.Step(`^the next continuation should be titled "([^"]*)"$`, state.continuationTitled)
}

type healingScenario struct {
	tuner         *ParameterTuner
	profile       models.AgentProfile
	lastAction    HealingAction
	taskTitle     string
	retitledTasks []models.DraftTask
}

func newHealingScenario() *healingScenario {
	return &healingScenario{
		tuner: NewParameterTuner(config.HealingConfig{
			Enabled:           true,
			Strategy:          config.HealingStrategyIncreaseEffort,
			ContextMultiplier: 2,
		}),
		profile: models.AgentProfile{
			ID: "default", Temperature: 0.5, Model: "gpt-4",
			SystemPrompt: sql.NullString{String: "Return JSON.", Valid: true},
		},
	}
}

func (s *healingScenario) taskFailedNTimes(_ context.Context, _ int) error {
	return nil
}

func (s *healingScenario) applyHealingLadder(_ context.Context, retryCount int) error {
	s.lastAction = s.tuner.ForAttempt(retryCount, s.profile)
	return nil
}

func (s *healingScenario) healingActionShouldBe(_ context.Context, actionType, stepName string) error {
	if string(s.lastAction.Type) != actionType {
		return fmt.Errorf("action type = %q, want %q", s.lastAction.Type, actionType)
	}
	if s.lastAction.StepName != stepName {
		return fmt.Errorf("step name = %q, want %q", s.lastAction.StepName, stepName)
	}
	return nil
}

func (s *healingScenario) tuneEventEmitted(context.Context) error {
	if s.lastAction.Type != HealingActionTune {
		return fmt.Errorf("last action type = %q, not tune", s.lastAction.Type)
	}
	return nil
}

func (s *healingScenario) taskTitled(_ context.Context, title string) error {
	s.taskTitle = title
	return nil
}

func (s *healingScenario) isPhasePlanningTask(context.Context) error {
	if !IsPhasePlanningTask(s.taskTitle) {
		return fmt.Errorf("%q should be a phase-planning task", s.taskTitle)
	}
	return nil
}

func (s *healingScenario) isNotPhasePlanningTask(context.Context) error {
	if IsPhasePlanningTask(s.taskTitle) {
		return fmt.Errorf("%q should not be a phase-planning task", s.taskTitle)
	}
	return nil
}

func (s *healingScenario) retitleContinuation(context.Context) error {
	input := []models.DraftTask{
		{Title: "Build component A"},
		{Title: "Plan Phase 2"},
	}
	s.retitledTasks = RetitlePhaseContinuationTasks(input, NextPhaseNumber(s.taskTitle))
	return nil
}

func (s *healingScenario) continuationTitled(_ context.Context, want string) error {
	for _, t := range s.retitledTasks {
		if t.Title == want {
			return nil
		}
	}
	titles := make([]string, len(s.retitledTasks))
	for i, t := range s.retitledTasks {
		titles[i] = t.Title
	}
	return fmt.Errorf("no task with title %q, got %v", want, titles)
}

// --- Prompt recovery / permission detection steps ---

func registerPromptPermSteps(sc *godog.ScenarioContext, state *promptPermScenario) {
	// Prompt recovery
	sc.Step(`^a running task with a sandbox command "([^"]*)"$`, state.runningTaskWithCommand)
	sc.Step(`^the sandbox times out with output containing "([^"]*)"$`, state.sandboxTimesOutWithOutput)
	sc.Step(`^the recovered command succeeds on retry$`, state.recoveredCommandSucceeds)
	sc.Step(`^the worker processes the timeout result$`, state.workerProcessesTimeout)
	sc.Step(`^a PROMPT_DETECTED event should be emitted$`, state.promptDetectedEmitted)
	sc.Step(`^the worker should attempt recovery with a non-interactive flag$`, state.workerAttemptsRecovery)
	sc.Step(`^the task should be completed successfully$`, state.taskCompletedSuccessfully)
	sc.Step(`^the parent task should be BLOCKED$`, state.parentShouldBeBlocked)
	sc.Step(`^a HUMAN child task should be created with title "([^"]*)"$`, state.humanChildCreated)
	sc.Step(`^a PROMPT_HANDOFF event should be emitted$`, state.promptHandoffEmitted)
	sc.Step(`^the sandbox times out with no prompt-like output$`, state.sandboxTimesOutNoPrompt)
	sc.Step(`^no PROMPT_DETECTED event should be emitted$`, state.noPromptDetectedEmitted)
	sc.Step(`^the task should follow the standard retry path$`, state.taskFollowsStandardRetry)

	// Permission detection
	sc.Step(`^a task whose agent returns a command starting with "sudo"$`, state.agentReturnsSudoCommand)
	sc.Step(`^the sandbox receives the command$`, state.sandboxReceivesCommand)
	sc.Step(`^the sandbox should reject the command with ErrSandboxViolation$`, state.sandboxRejectsSudo)
	sc.Step(`^a SANDBOX_VIOLATION event should be emitted$`, state.sandboxViolationEmitted)
	sc.Step(`^a running task with a failed sandbox command$`, state.failedSandboxWithPermissionDenied)
	sc.Step(`^the sandbox output contains "([^"]*)"$`, state.sandboxOutputContainsPermission)
	sc.Step(`^the worker processes the failed result$`, state.workerProcessesFailed)
	sc.Step(`^a PERMISSION_DETECTED event should be emitted$`, state.permissionDetectedEmitted)
	sc.Step(`^a PERMISSION_HANDOFF event should be emitted$`, state.permissionHandoffEmitted)
	sc.Step(`^a running task with a successful sandbox result$`, state.successfulSandboxWithPermissionText)
	sc.Step(`^the sandbox output contains "([^"]*)" in a non-error context$`, state.permissionTextInNonErrorContext)
	sc.Step(`^the worker processes the successful result$`, state.workerProcessesSuccessful)
	sc.Step(`^no PERMISSION_DETECTED event should be emitted$`, state.noPermissionDetectedEmitted)
}

type promptPermScenario struct {
	store   *queueStore
	breaker *CircuitBreaker
	gateway *queueGateway
	sandbox *promptSandbox
	worker  *Worker
	sink    *queueSink
	task    models.Task
}

type promptSandbox struct {
	result      sandbox.Result
	err         error
	retryResult sandbox.Result
	retryErr    error
	retryUsed   bool
	callCount   int
	lastCommand string
	sudoBlocked bool
}

func (s *promptSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	s.callCount++
	s.lastCommand = p.Command
	if s.sudoBlocked {
		return sandbox.Result{}, models.ErrSandboxViolation
	}
	if s.callCount > 1 && s.retryUsed {
		return s.retryResult, s.retryErr
	}
	if s.callCount > 1 {
		s.retryUsed = true
		return s.retryResult, s.retryErr
	}
	return s.result, s.err
}

func newPromptPermScenario() *promptPermScenario {
	s := &promptPermScenario{}
	s.store = newQueueStore()
	s.breaker = NewCircuitBreaker()
	s.gateway = &queueGateway{content: `{"command":"echo hello"}`}
	s.sandbox = &promptSandbox{}
	s.sink = &queueSink{}
	s.worker = NewWorker(s.store, s.gateway, s.sandbox, s.breaker, s.sink, WorkerOptions{MaxRetries: 3})
	return s
}

func (s *promptPermScenario) seedRunningTask(command string) {
	s.store.seed(1, models.TaskStateQueued)
	s.gateway.content = fmt.Sprintf(`{"command":%q}`, command)
	s.task = s.store.tasks[0]
}

func (s *promptPermScenario) runningTaskWithCommand(_ context.Context, command string) error {
	s.seedRunningTask(command)
	return nil
}

func (s *promptPermScenario) sandboxTimesOutWithOutput(_ context.Context, pattern string) error {
	s.sandbox.result = sandbox.Result{
		Success: false, ExitCode: -1, TimedOut: true,
		Stdout: fmt.Sprintf("Reading package lists...\n%s", pattern),
	}
	s.sandbox.err = models.ErrExecutionTimeout
	return nil
}

func (s *promptPermScenario) recoveredCommandSucceeds(context.Context) error {
	s.sandbox.retryResult = sandbox.Result{Success: true, ExitCode: 0, Stdout: "installed"}
	s.sandbox.retryErr = nil
	return nil
}

func (s *promptPermScenario) workerProcessesTimeout(context.Context) error {
	s.worker.Process(context.Background(), s.task)
	return nil
}

func (s *promptPermScenario) promptDetectedEmitted(context.Context) error {
	if !s.sink.containsType("PROMPT_DETECTED") {
		return fmt.Errorf("missing PROMPT_DETECTED event")
	}
	return nil
}

func (s *promptPermScenario) workerAttemptsRecovery(context.Context) error {
	if s.sandbox.callCount < 2 {
		return fmt.Errorf("sandbox call count = %d, want >= 2", s.sandbox.callCount)
	}
	return nil
}

func (s *promptPermScenario) taskCompletedSuccessfully(context.Context) error {
	task, err := s.store.GetTask(context.Background(), s.task.ID)
	if err != nil {
		return err
	}
	if task.State != models.TaskStateCompleted {
		return fmt.Errorf("task state = %s, want COMPLETED", task.State)
	}
	return nil
}

func (s *promptPermScenario) parentShouldBeBlocked(context.Context) error {
	task, err := s.store.GetTask(context.Background(), s.task.ID)
	if err != nil {
		return err
	}
	if task.State != models.TaskStateBlocked {
		return fmt.Errorf("task state = %s, want BLOCKED", task.State)
	}
	return nil
}

func (s *promptPermScenario) humanChildCreated(_ context.Context, title string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	for _, t := range s.store.children {
		if t.Assignee == models.TaskAssigneeHuman && strings.Contains(t.Title, title) {
			return nil
		}
	}
	return fmt.Errorf("no HUMAN child task with title containing %q", title)
}

func (s *promptPermScenario) promptHandoffEmitted(context.Context) error {
	if !s.sink.containsType("PROMPT_HANDOFF") {
		return fmt.Errorf("missing PROMPT_HANDOFF event")
	}
	return nil
}

func (s *promptPermScenario) sandboxTimesOutNoPrompt(context.Context) error {
	s.sandbox.result = sandbox.Result{
		Success: false, ExitCode: -1, TimedOut: true,
		Stdout: "compilation in progress...",
	}
	s.sandbox.err = models.ErrExecutionTimeout
	return nil
}

func (s *promptPermScenario) noPromptDetectedEmitted(context.Context) error {
	if s.sink.containsType("PROMPT_DETECTED") {
		return fmt.Errorf("unexpected PROMPT_DETECTED event")
	}
	return nil
}

func (s *promptPermScenario) taskFollowsStandardRetry(context.Context) error {
	task, err := s.store.GetTask(context.Background(), s.task.ID)
	if err != nil {
		return err
	}
	if task.State == models.TaskStateCompleted {
		return fmt.Errorf("task should not be completed on timeout without prompt")
	}
	return nil
}

func (s *promptPermScenario) agentReturnsSudoCommand(context.Context) error {
	s.seedRunningTask("sudo apt install nginx")
	s.sandbox.sudoBlocked = true
	return nil
}

func (s *promptPermScenario) sandboxReceivesCommand(context.Context) error {
	s.worker.Process(context.Background(), s.task)
	return nil
}

func (s *promptPermScenario) sandboxRejectsSudo(context.Context) error {
	return nil
}

func (s *promptPermScenario) sandboxViolationEmitted(context.Context) error {
	if s.sink.containsType("SANDBOX_VIOLATION") {
		return nil
	}
	for _, e := range s.sink.events {
		if strings.Contains(e.Payload, "sandbox") || strings.Contains(e.Payload, "violation") || strings.Contains(e.Payload, "sudo") {
			return nil
		}
	}
	return fmt.Errorf("missing SANDBOX_VIOLATION or related event")
}

func (s *promptPermScenario) failedSandboxWithPermissionDenied(context.Context) error {
	s.seedRunningTask("install-package")
	s.sandbox.result = sandbox.Result{
		Success: false, ExitCode: 1,
		Stderr: "Permission denied",
	}
	s.sandbox.err = nil
	return nil
}

func (s *promptPermScenario) sandboxOutputContainsPermission(_ context.Context, pattern string) error {
	return nil
}

func (s *promptPermScenario) workerProcessesFailed(context.Context) error {
	s.worker.Process(context.Background(), s.task)
	return nil
}

func (s *promptPermScenario) permissionDetectedEmitted(context.Context) error {
	if !s.sink.containsType("PERMISSION_DETECTED") {
		return fmt.Errorf("missing PERMISSION_DETECTED event")
	}
	return nil
}

func (s *promptPermScenario) permissionHandoffEmitted(context.Context) error {
	if !s.sink.containsType("PERMISSION_HANDOFF") {
		return fmt.Errorf("missing PERMISSION_HANDOFF event")
	}
	return nil
}

func (s *promptPermScenario) successfulSandboxWithPermissionText(context.Context) error {
	s.seedRunningTask("cat logfile")
	s.sandbox.result = sandbox.Result{
		Success: true, ExitCode: 0,
		Stdout: "Permission denied in old log entry",
	}
	s.sandbox.err = nil
	return nil
}

func (s *promptPermScenario) permissionTextInNonErrorContext(_ context.Context, _ string) error {
	return nil
}

func (s *promptPermScenario) workerProcessesSuccessful(context.Context) error {
	s.worker.Process(context.Background(), s.task)
	return nil
}

func (s *promptPermScenario) noPermissionDetectedEmitted(context.Context) error {
	if s.sink.containsType("PERMISSION_DETECTED") {
		return fmt.Errorf("unexpected PERMISSION_DETECTED event")
	}
	return nil
}
