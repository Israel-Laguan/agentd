package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

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
	lastErr     error
	sudoBlocked bool
}

func (s *promptSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	s.callCount++
	s.lastCommand = p.Command
	if s.sudoBlocked {
		s.lastErr = models.ErrSandboxViolation
		return sandbox.Result{}, s.lastErr
	}
	if s.callCount > 1 && s.retryUsed {
		s.lastErr = s.retryErr
		return s.retryResult, s.retryErr
	}
	if s.callCount > 1 {
		s.retryUsed = true
		s.lastErr = s.retryErr
		return s.retryResult, s.retryErr
	}
	s.lastErr = s.err
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
	if !errors.Is(s.sandbox.lastErr, models.ErrSandboxViolation) {
		return fmt.Errorf("sandbox err = %v, want %v", s.sandbox.lastErr, models.ErrSandboxViolation)
	}
	if !strings.HasPrefix(strings.TrimSpace(s.sandbox.lastCommand), "sudo") {
		return fmt.Errorf("sandbox command = %q, want sudo-prefixed command", s.sandbox.lastCommand)
	}
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
	combined := s.sandbox.result.Stdout + "\n" + s.sandbox.result.Stderr
	if !strings.Contains(combined, pattern) {
		return fmt.Errorf("sandbox output missing %q in %q", pattern, combined)
	}
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

func (s *promptPermScenario) permissionTextInNonErrorContext(_ context.Context, pattern string) error {
	if !s.sandbox.result.Success {
		return fmt.Errorf("sandbox result should be successful for non-error context")
	}
	if !strings.Contains(s.sandbox.result.Stdout, pattern) {
		return fmt.Errorf("stdout missing %q in %q", pattern, s.sandbox.result.Stdout)
	}
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
