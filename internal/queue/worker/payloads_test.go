package worker

import (
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

func TestFailurePayloadPrefersError(t *testing.T) {
	got := failurePayload(sandbox.Result{Stdout: "out", Stderr: "err"}, errors.New("boom"))
	if got != "boom" {
		t.Fatalf("got %q", got)
	}
}

func TestFailurePayloadFromResult(t *testing.T) {
	got := failurePayload(sandbox.Result{Stdout: " a ", Stderr: " b "}, nil)
	if got != "b \n a" {
		t.Fatalf("got %q", got)
	}
}

func TestResultPayload(t *testing.T) {
	d := 2 * time.Second
	got := resultPayload(sandbox.Result{ExitCode: 7, Duration: d, Stdout: "ok\n"})
	if got != "exit=7 duration=2s\nok\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateHelper(t *testing.T) {
	if got := truncate("  hi  ", 10); got != "hi" {
		t.Fatalf("got %q", got)
	}
	s := strings.Repeat("z", 30)
	got := truncate(s, 10)
	if !strings.HasPrefix(got, strings.Repeat("z", 10)) || !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("truncate long: %q", got)
	}
}

func TestPromptAndPermissionPayload(t *testing.T) {
	res := sandbox.Result{ExitCode: 1, Duration: time.Second, Stderr: "e", Stdout: "o"}
	pd := safety.PromptDetection{Pattern: "sudo"}
	got := promptPayload("ls", pd, res)
	if got == "" {
		t.Fatal("empty prompt payload")
	}
	pp := safety.PermissionDetection{Pattern: "denied"}
	got2 := permissionPayload("x", pp, res)
	if got2 == "" {
		t.Fatal("empty permission payload")
	}
}
