package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// --- ScrubResultHook tests ---

func TestScrubResultHook_RedactsAPIKey(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber(nil)
	hook := ScrubResultHook(scrubber)

	input := "output with sk-AAAAAAAAAAAAAAAAAAAAAA key"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "sk-AAAA") {
		t.Fatalf("expected API key to be scrubbed, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] token, got %q", got)
	}
}

func TestScrubResultHook_NilScrubberPassthrough(t *testing.T) {
	t.Parallel()
	hook := ScrubResultHook(nil)
	input := "pass through sk-SECRET123"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Fatalf("nil scrubber should passthrough, got %q", got)
	}
}

func TestScrubResultHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := ScrubResultHook(sandbox.NewScrubber(nil))
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed, got %v", hook.Policy)
	}
}

func TestScrubResultHook_CleanInputUnchanged(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber(nil)
	hook := ScrubResultHook(scrubber)
	input := "clean output with no secrets"
	got, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Fatalf("clean input should be unchanged, got %q", got)
	}
}

func TestScrubResultHook_IntegrationViaHookChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(ScrubResultHook(sandbox.NewScrubber(nil)))

	result := hc.RunPost(
		HookContext{ToolName: "bash", Timestamp: time.Now()},
		"token=sk-AAAAAAAAAAAAAAAAAAAAAA found",
	)
	if strings.Contains(result, "sk-AAAA") {
		t.Fatalf("API key not scrubbed through hook chain: %q", result)
	}
}

func TestScrubResultHook_ScrubsBeforeModelContext(t *testing.T) {
	t.Parallel()

	scrubber := sandbox.NewScrubber(nil)
	hc := NewHookChain()
	hc.RegisterPost(ScrubResultHook(scrubber))

	mockSB := &mockExecSandbox{result: sandbox.Result{Stdout: "key=sk-AAAAAAAAAAAAAAAAAAAAAA\n", Success: true}}
	executor := NewToolExecutor(mockSB, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo key"}`},
	}

	tr := w.DispatchTool(context.Background(), "test-session", call, nil, executor)
	if strings.Contains(tr.Content, "sk-AAAA") {
		t.Fatalf("expected scrubbed result before model context, got %q", tr.Content)
	}
	if !strings.Contains(tr.Content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in result, got %q", tr.Content)
	}
}

func TestScrubResultHook_CustomPatterns(t *testing.T) {
	t.Parallel()
	scrubber := sandbox.NewScrubber([]string{`my-custom-secret-\d+`})
	hook := ScrubResultHook(scrubber)

	input := "found my-custom-secret-42 in config"
	got, err := hook.Fn(HookContext{ToolName: "read", Timestamp: time.Now()}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "my-custom-secret-42") {
		t.Fatalf("custom pattern not scrubbed: %q", got)
	}
}
