package queue

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestWorkerPromptHangRetriesRecoverableCommandOnce(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"apt-get install foo"}`}
	sb := &fakeSandbox{
		results: []sandbox.Result{
			{TimedOut: true, ExitCode: -1, Stdout: "Install foo? [y/N]"},
			{Success: true, ExitCode: 0, Stdout: "ok"},
		},
		errs: []error{models.ErrExecutionTimeout, nil},
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.task.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", store.task.RetryCount)
	}
	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v", store.result)
	}
	if len(sb.commands) != 2 || sb.commands[1] != "apt-get -y install foo" {
		t.Fatalf("commands = %#v", sb.commands)
	}
}

func TestWorkerPromptHangCreatesHumanTaskForNonRecoverableCommand(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"ssh example.com"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{TimedOut: true, ExitCode: -1, Stderr: "Password:"},
		err:    models.ErrExecutionTimeout,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 1 {
		t.Fatalf("appends = %d, want 1", store.appends)
	}
	if store.task.State != models.TaskStateBlocked || store.result != nil {
		t.Fatalf("state=%s result=%#v", store.task.State, store.result)
	}
	if len(store.drafts) != 1 || store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("drafts = %#v", store.drafts)
	}
}

func TestWorkerTimeoutWithoutPromptUsesExistingRetryFlow(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"sleep 5"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{TimedOut: true, ExitCode: -1},
		err:    models.ErrExecutionTimeout,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 0 || store.task.RetryCount != 1 || store.task.State != models.TaskStateReady {
		t.Fatalf("appends=%d retries=%d state=%s", store.appends, store.task.RetryCount, store.task.State)
	}
}

func TestWorkerPromptHangAlreadyRetriedCreatesHumanTask(t *testing.T) {
	store := newWorkerStore()
	store.task.RetryCount = 1
	gw := &fakeGateway{content: `{"command":"apt-get install foo"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{TimedOut: true, ExitCode: -1, Stdout: "Install foo? [y/N]"},
		err:    models.ErrExecutionTimeout,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 1 {
		t.Fatalf("appends = %d, want 1", store.appends)
	}
	if len(sb.commands) != 1 {
		t.Fatalf("commands = %#v", sb.commands)
	}
}

func TestWorkerPermissionFailureCreatesHumanTask(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"touch /etc/agentd.conf"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{Success: false, ExitCode: 1, Stderr: "touch: /etc/agentd.conf: Permission denied"},
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 1 {
		t.Fatalf("appends = %d, want 1", store.appends)
	}
	if len(store.drafts) != 1 || store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("drafts = %#v", store.drafts)
	}
	if !strings.Contains(store.drafts[0].Description, "touch /etc/agentd.conf") {
		t.Fatalf("description = %q", store.drafts[0].Description)
	}
	if store.task.State != models.TaskStateBlocked || store.result != nil {
		t.Fatalf("state=%s result=%#v", store.task.State, store.result)
	}
}

func TestWorkerSudoBlockExecutesOneNonPrivilegedAlternative(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{
		content:     `{"command":"sudo dmidecode -t memory"}`,
		nextContent: `{"command":"free -h"}`,
	}
	sb := &fakeSandbox{
		results: []sandbox.Result{
			{Success: false, ExitCode: -1, Stderr: "sudo command blocked"},
			{Success: true, Stdout: "available without privileges"},
		},
		errs: []error{models.ErrSandboxViolation, nil},
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if len(sb.commands) != 2 || sb.commands[0] != "sudo dmidecode -t memory" || sb.commands[1] != "free -h" {
		t.Fatalf("commands = %#v", sb.commands)
	}
	if len(gw.requests) != 2 {
		t.Fatalf("gateway requests = %d, want initial command plus one alternative request", len(gw.requests))
	}
	if store.appends != 0 || store.task.State != models.TaskStateCompleted {
		t.Fatalf("appends=%d state=%s, want completed without handoff", store.appends, store.task.State)
	}
	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v", store.result)
	}
}

func TestWorkerSudoBlockRejectsSudoAlternativeWithoutSecondExecution(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{
		content:     `{"command":"sudo dmidecode -t memory"}`,
		nextContent: `{"command":"sudo cat /proc/meminfo"}`,
	}
	sb := &fakeSandbox{
		result: sandbox.Result{Success: false, ExitCode: -1, Stderr: "sudo command blocked"},
		err:    models.ErrSandboxViolation,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if len(sb.commands) != 1 || !strings.HasPrefix(sb.commands[0], "sudo ") {
		t.Fatalf("commands = %#v, want only the original sudo command", sb.commands)
	}
	if len(gw.requests) != 2 {
		t.Fatalf("gateway requests = %d, want one bounded alternative request", len(gw.requests))
	}
	if store.appends != 1 || len(store.drafts) != 1 || store.drafts[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("handoff appends=%d drafts=%#v", store.appends, store.drafts)
	}
}

func TestWorkerPermissionFailureFromSandboxViolationCreatesHumanTask(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"command":"sudo touch /etc/agentd.conf"}`}
	sb := &fakeSandbox{
		result: sandbox.Result{Success: false, ExitCode: -1, Stderr: "sudo command blocked"},
		err:    models.ErrSandboxViolation,
	}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 1 {
		t.Fatalf("appends = %d, want 1", store.appends)
	}
	if store.task.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", store.task.RetryCount)
	}
}
