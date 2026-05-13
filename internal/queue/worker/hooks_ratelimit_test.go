package worker

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// --- RateLimitStore unit tests ---

func TestRateLimitStore_IncrementAndCount(t *testing.T) {
	t.Parallel()
	s := NewRateLimitStore()
	if got := s.Count("bash"); got != 0 {
		t.Fatalf("Count(bash) = %d, want 0", got)
	}
	if got := s.Increment("bash"); got != 1 {
		t.Fatalf("first Increment = %d, want 1", got)
	}
	if got := s.Increment("bash"); got != 2 {
		t.Fatalf("second Increment = %d, want 2", got)
	}
	if got := s.Count("bash"); got != 2 {
		t.Fatalf("Count(bash) = %d, want 2", got)
	}
}

func TestRateLimitStore_IsolatedTools(t *testing.T) {
	t.Parallel()
	s := NewRateLimitStore()
	s.Increment("bash")
	s.Increment("bash")
	s.Increment("read")
	if got := s.Count("bash"); got != 2 {
		t.Fatalf("Count(bash) = %d, want 2", got)
	}
	if got := s.Count("read"); got != 1 {
		t.Fatalf("Count(read) = %d, want 1", got)
	}
}

func TestRateLimitStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	s := NewRateLimitStore()
	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			s.Increment("bash")
		}()
	}
	wg.Wait()
	if got := s.Count("bash"); got != n {
		t.Fatalf("Count(bash) = %d, want %d", got, n)
	}
}

// --- resolveLimit ---

func TestResolveLimit_ExplicitTool(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 10, "default": 100}
	if got := resolveLimit(limits, "bash"); got != 10 {
		t.Fatalf("resolveLimit(bash) = %d, want 10", got)
	}
}

func TestResolveLimit_FallsBackToDefault(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 10, "default": 100}
	if got := resolveLimit(limits, "read"); got != 100 {
		t.Fatalf("resolveLimit(read) = %d, want 100", got)
	}
}

func TestResolveLimit_NoDefault(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 10}
	if got := resolveLimit(limits, "read"); got != 0 {
		t.Fatalf("resolveLimit(read) = %d, want 0", got)
	}
}

// --- RateLimitHook ---

func TestRateLimitHook_VetoesOnExceed(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 3}
	store := NewRateLimitStore()
	hook := RateLimitHook(limits, store)

	ctx := HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()}
	for i := 1; i <= 3; i++ {
		verdict, err := hook.Fn(ctx)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if verdict.Veto {
			t.Fatalf("call %d: unexpected veto", i)
		}
	}

	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("call 4: unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto on 4th call")
	}
	if !strings.Contains(verdict.Reason, "3/3") {
		t.Fatalf("reason should contain 3/3, got %q", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "bash") {
		t.Fatalf("reason should mention tool name, got %q", verdict.Reason)
	}
}

func TestRateLimitHook_UsesDefaultLimit(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"default": 2}
	store := NewRateLimitStore()
	hook := RateLimitHook(limits, store)

	ctx := HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}
	for i := 1; i <= 2; i++ {
		verdict, err := hook.Fn(ctx)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if verdict.Veto {
			t.Fatalf("call %d: unexpected veto", i)
		}
	}

	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("call 3: unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto on 3rd call with default limit=2")
	}
}

func TestRateLimitHook_PerSessionIsolation(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 2}
	store1 := NewRateLimitStore()
	store2 := NewRateLimitStore()
	hook1 := RateLimitHook(limits, store1)
	hook2 := RateLimitHook(limits, store2)

	ctx := HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()}

	// Exhaust store1
	for i := 0; i < 2; i++ {
		if _, err := hook1.Fn(ctx); err != nil {
			t.Fatalf("unexpected error exhausting store1: %v", err)
		}
	}

	// store2 should still be fresh
	verdict, err := hook2.Fn(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("separate store should not be affected by other session")
	}
}

func TestRateLimitHook_NilStore(t *testing.T) {
	t.Parallel()
	hook := RateLimitHook(map[string]int{"bash": 1}, nil)
	verdict, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("nil store should not veto")
	}
}

func TestRateLimitHook_EmptyLimits(t *testing.T) {
	t.Parallel()
	hook := RateLimitHook(nil, NewRateLimitStore())
	verdict, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("empty limits should not veto")
	}
}

func TestRateLimitHook_ZeroLimit_Unlimited(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 0, "default": 0}
	store := NewRateLimitStore()
	hook := RateLimitHook(limits, store)

	ctx := HookContext{ToolName: "bash", Timestamp: time.Now()}
	for i := 0; i < 100; i++ {
		verdict, err := hook.Fn(ctx)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if verdict.Veto {
			t.Fatalf("call %d: zero limit should mean unlimited", i)
		}
	}
}

func TestRateLimitHook_NegativeLimit_Vetoes(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": -5}
	store := NewRateLimitStore()
	hook := RateLimitHook(limits, store)

	verdict, err := hook.Fn(HookContext{ToolName: "bash", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("negative limit should veto")
	}
	if !strings.Contains(verdict.Reason, "Invalid negative rate limit") {
		t.Fatalf("reason should mention invalid negative limit, got %q", verdict.Reason)
	}
}

func TestRateLimitHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := RateLimitHook(map[string]int{"bash": 1}, NewRateLimitStore())
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed, got %d", hook.Policy)
	}
}

func TestRateLimitHook_IntegrationViaHookChain(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 2}
	store := NewRateLimitStore()
	hc := NewHookChain()
	hc.RegisterPre(RateLimitHook(limits, store))

	ctx := HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()}

	for i := 0; i < 2; i++ {
		v := hc.RunPre(ctx)
		if v.Veto {
			t.Fatalf("call %d: unexpected veto via chain", i)
		}
	}

	v := hc.RunPre(ctx)
	if !v.Veto {
		t.Fatal("expected veto on 3rd call via chain")
	}
}

func TestRateLimitHook_MessageFormat(t *testing.T) {
	t.Parallel()
	limits := map[string]int{"bash": 1}
	store := NewRateLimitStore()
	hook := RateLimitHook(limits, store)

	ctx := HookContext{ToolName: "bash", Timestamp: time.Now()}
	if _, err := hook.Fn(ctx); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	verdict, err := hook.Fn(ctx) // second call vetoed
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	want := `Rate limit exceeded for tool "bash" (1/1). Consider consolidating commands.`
	if verdict.Reason != want {
		t.Fatalf("reason mismatch:\n  got:  %s\n  want: %s", verdict.Reason, want)
	}
}
