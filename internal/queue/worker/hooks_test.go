package worker

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHookChain_RunPre_Passthrough(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{
		Name:   "allow",
		Policy: FailOpen,
		Fn:     func(HookContext) (HookVerdict, error) { return HookVerdict{}, nil },
	})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if verdict.Veto {
		t.Fatal("expected passthrough, got veto")
	}
}

func TestHookChain_RunPre_Veto(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{
		Name:   "blocker",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, Reason: "blocked"}, nil
		},
	})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if !verdict.Veto {
		t.Fatal("expected veto")
	}
	if verdict.Reason != "blocked" {
		t.Fatalf("expected reason 'blocked', got %q", verdict.Reason)
	}
}

func TestHookChain_RunPre_ShortCircuits(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	called := false
	hc.RegisterPre(PreHook{
		Name:   "blocker",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, Reason: "first"}, nil
		},
	})
	hc.RegisterPre(PreHook{
		Name:   "second",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			called = true
			return HookVerdict{}, nil
		},
	})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if !verdict.Veto {
		t.Fatal("expected veto from first hook")
	}
	if called {
		t.Fatal("second hook should not have been called after veto")
	}
}

func TestHookChain_RunPre_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{
		Name:   "security",
		Policy: FailClosed,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{}, errors.New("check failed")
		},
	})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if !verdict.Veto {
		t.Fatal("expected veto on fail_closed error")
	}
	if !strings.Contains(verdict.Reason, "fail_closed") {
		t.Fatalf("expected reason to contain 'fail_closed', got %q", verdict.Reason)
	}
}

func TestHookChain_RunPre_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(PreHook{
		Name:   "lint",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{}, errors.New("lint crash")
		},
	})

	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if verdict.Veto {
		t.Fatal("expected passthrough on fail_open error")
	}
}

func TestHookChain_RunPre_EmptyChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	verdict := hc.RunPre(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if verdict.Veto {
		t.Fatal("empty chain should not veto")
	}
}

func TestHookChain_RunPost_Passthrough(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "noop",
		Policy: FailOpen,
		Fn:     func(_ HookContext, result string) (string, error) { return result, nil },
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if got != "original" {
		t.Fatalf("expected 'original', got %q", got)
	}
}

func TestHookChain_RunPost_Mutation(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "redact",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			return strings.ReplaceAll(result, "secret", "[REDACTED]"), nil
		},
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "the secret value")
	if got != "the [REDACTED] value" {
		t.Fatalf("expected 'the [REDACTED] value', got %q", got)
	}
}

func TestHookChain_RunPost_ChainedMutation(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "upper",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			return strings.ToUpper(result), nil
		},
	})
	hc.RegisterPost(PostHook{
		Name:   "prefix",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			return ">> " + result, nil
		},
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "hello")
	if got != ">> HELLO" {
		t.Fatalf("expected '>> HELLO', got %q", got)
	}
}

func TestHookChain_RunPost_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "critical",
		Policy: FailClosed,
		Fn: func(_ HookContext, _ string) (string, error) {
			return "", errors.New("post-process failed")
		},
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if !strings.Contains(got, "fail_closed") {
		t.Fatalf("expected fail_closed error message, got %q", got)
	}
}

func TestHookChain_RunPost_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPost(PostHook{
		Name:   "optional",
		Policy: FailOpen,
		Fn: func(_ HookContext, _ string) (string, error) {
			return "", errors.New("optional crash")
		},
	})

	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "original")
	if got != "original" {
		t.Fatalf("expected 'original' on fail_open, got %q", got)
	}
}

func TestHookChain_RunPost_EmptyChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	got := hc.RunPost(HookContext{ToolName: "bash", Timestamp: time.Now()}, "unchanged")
	if got != "unchanged" {
		t.Fatalf("expected 'unchanged', got %q", got)
	}
}

func TestHookChain_RunSessionStart_Success(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	ran := false
	hc.RegisterSessionStart(SessionStartHook{
		Name:   "env-check",
		Policy: FailOpen,
		Fn:     func(HookContext) error { ran = true; return nil },
	})

	err := hc.RunSessionStart(HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("hook did not run")
	}
}

func TestHookChain_RunSessionStart_FailClosed(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterSessionStart(SessionStartHook{
		Name:   "creds",
		Policy: FailClosed,
		Fn:     func(HookContext) error { return errors.New("missing creds") },
	})

	err := hc.RunSessionStart(HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err == nil {
		t.Fatal("expected error on fail_closed")
	}
}

func TestHookChain_RunSessionStart_FailOpen(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	secondRan := false
	hc.RegisterSessionStart(SessionStartHook{
		Name:   "optional",
		Policy: FailOpen,
		Fn:     func(HookContext) error { return errors.New("crash") },
	})
	hc.RegisterSessionStart(SessionStartHook{
		Name:   "second",
		Policy: FailOpen,
		Fn:     func(HookContext) error { secondRan = true; return nil },
	})

	err := hc.RunSessionStart(HookContext{SessionID: "s1", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error on fail_open: %v", err)
	}
	if !secondRan {
		t.Fatal("second hook should have run after fail_open error")
	}
}

func TestHookContext_CarriesFields(t *testing.T) {
	t.Parallel()
	ts := time.Now()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		SessionID: "sess-42",
		Timestamp: ts,
	}
	if ctx.ToolName != "bash" {
		t.Fatalf("ToolName = %q", ctx.ToolName)
	}
	if ctx.Args != `{"command":"ls"}` {
		t.Fatalf("Args = %q", ctx.Args)
	}
	if ctx.SessionID != "sess-42" {
		t.Fatalf("SessionID = %q", ctx.SessionID)
	}
	if !ctx.Timestamp.Equal(ts) {
		t.Fatalf("Timestamp mismatch")
	}
}
