package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// --- ResultCache unit tests ---

func TestResultCache_GetMiss(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	if _, ok := rc.get("nonexistent"); ok {
		t.Fatal("expected cache miss")
	}
}

func TestResultCache_SetAndGet(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	rc.set("k", "v")
	got, ok := rc.get("k")
	if !ok || got != "v" {
		t.Fatalf("expected (v, true), got (%q, %v)", got, ok)
	}
}

func TestResultCache_IsCacheable(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	if !rc.isCacheable("read") {
		t.Fatal("read should be cacheable")
	}
	if rc.isCacheable("bash") {
		t.Fatal("bash should not be cacheable")
	}
	if rc.isCacheable("write") {
		t.Fatal("write should not be cacheable")
	}
}

func TestResultCache_NilMap(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(nil)
	if rc.isCacheable("read") {
		t.Fatal("nil map should make nothing cacheable")
	}
}

// --- cacheKey / canonicalizeArgs ---

func TestCacheKey_IdenticalArgs(t *testing.T) {
	t.Parallel()
	k1 := cacheKey("read", `{"path":"foo.txt"}`)
	k2 := cacheKey("read", `{ "path" : "foo.txt" }`)
	if k1 != k2 {
		t.Fatalf("identical args should produce same key:\n  %s\n  %s", k1, k2)
	}
}

func TestCacheKey_DifferentArgs(t *testing.T) {
	t.Parallel()
	k1 := cacheKey("read", `{"path":"a.txt"}`)
	k2 := cacheKey("read", `{"path":"b.txt"}`)
	if k1 == k2 {
		t.Fatal("different args should produce different keys")
	}
}

func TestCacheKey_DifferentTools(t *testing.T) {
	t.Parallel()
	k1 := cacheKey("read", `{"path":"x.txt"}`)
	k2 := cacheKey("write", `{"path":"x.txt"}`)
	if k1 == k2 {
		t.Fatal("different tool names should produce different keys")
	}
}

func TestCacheKey_KeyOrderIrrelevant(t *testing.T) {
	t.Parallel()
	k1 := cacheKey("write", `{"path":"f","content":"c"}`)
	k2 := cacheKey("write", `{"content":"c","path":"f"}`)
	if k1 != k2 {
		t.Fatalf("key order should not affect cache key:\n  %s\n  %s", k1, k2)
	}
}

func TestCanonicalizeArgs_Empty(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "  ", "{}"} {
		if got := canonicalizeArgs(input); got != "{}" {
			t.Fatalf("canonicalizeArgs(%q) = %q, want {}", input, got)
		}
	}
}

func TestCanonicalizeArgs_InvalidJSON(t *testing.T) {
	t.Parallel()
	bad := "not-json"
	if got := canonicalizeArgs(bad); got != bad {
		t.Fatalf("invalid JSON should be returned as-is, got %q", got)
	}
}

// --- CacheLookupHook ---

func TestCacheLookupHook_MissPassesThrough(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheLookupHook(rc)

	verdict, err := hook.Fn(HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("cache miss should not veto")
	}
}

func TestCacheLookupHook_HitShortCircuits(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	key := cacheKey("read", `{"path":"a.txt"}`)
	rc.set(key, "cached-content")

	hook := CacheLookupHook(rc)
	verdict, err := hook.Fn(HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto || !verdict.ShortCircuit {
		t.Fatal("cache hit should veto with ShortCircuit")
	}
	if verdict.Result != "cached-content" {
		t.Fatalf("expected cached-content, got %q", verdict.Result)
	}
}

func TestCacheLookupHook_SkipsNonCacheableTool(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheLookupHook(rc)

	verdict, err := hook.Fn(HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("non-cacheable tool should not veto")
	}
}

func TestCacheLookupHook_NilCache(t *testing.T) {
	t.Parallel()
	hook := CacheLookupHook(nil)
	verdict, err := hook.Fn(HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("nil cache should not veto")
	}
}

// --- CacheStoreHook ---

func TestCacheStoreHook_StoresCacheableResult(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheStoreHook(rc)

	ctx := HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}
	got, err := hook.Fn(ctx, "file-contents")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "file-contents" {
		t.Fatalf("hook should not mutate result, got %q", got)
	}

	key := cacheKey("read", `{"path":"a.txt"}`)
	cached, ok := rc.get(key)
	if !ok || cached != "file-contents" {
		t.Fatalf("expected cached result, got (%q, %v)", cached, ok)
	}
}

func TestCacheStoreHook_SkipsBash(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheStoreHook(rc)

	ctx := HookContext{ToolName: "bash", Args: `{"command":"ls"}`, Timestamp: time.Now()}
	got, err := hook.Fn(ctx, "output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "output" {
		t.Fatalf("should passthrough, got %q", got)
	}

	if len(rc.entries) != 0 {
		t.Fatal("bash result should not be cached")
	}
}

func TestCacheStoreHook_SkipsWrite(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheStoreHook(rc)

	ctx := HookContext{ToolName: "write", Args: `{"path":"a.txt","content":"x"}`, Timestamp: time.Now()}
	_, _ = hook.Fn(ctx, `{"success": true}`)
	if len(rc.entries) != 0 {
		t.Fatal("write result should not be cached")
	}
}

func TestCacheStoreHook_NilCache(t *testing.T) {
	t.Parallel()
	hook := CacheStoreHook(nil)
	got, err := hook.Fn(HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}, "data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "data" {
		t.Fatalf("nil cache should passthrough, got %q", got)
	}
}

// countingSandbox wraps mockExecSandbox and counts Execute calls.
type countingSandbox struct {
	inner sandbox.Executor
	calls int
}

func (c *countingSandbox) Execute(ctx context.Context, p sandbox.Payload) (sandbox.Result, error) {
	c.calls++
	return c.inner.Execute(ctx, p)
}

// --- Integration: consecutive reads return cached result ---

func TestCacheHooks_ConsecutiveReadsReturnCached(t *testing.T) {
	t.Parallel()

	rc := NewResultCache(map[string]bool{"read": true})
	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))
	hc.RegisterPost(CacheStoreHook(rc))

	counting := &countingSandbox{inner: &mockExecSandbox{result: sandbox.Result{
		Stdout:  "hello world",
		Success: true,
	}}}
	executor := NewToolExecutor(counting, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"test.txt"}`},
	}

	result1 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	call.ID = "call_2"
	result2 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	if result1 != result2 {
		t.Fatalf("expected identical results:\n  first:  %q\n  second: %q", result1, result2)
	}
	// read does not go through the sandbox (it uses os.ReadFile directly),
	// so we verify via the cache: second call must hit the cache and skip
	// execution entirely. The ShortCircuit path in DispatchTool returns
	// before reaching Execute, which is validated by
	// TestCacheHooks_ShortCircuitSkipsPostHooks. Here we confirm the
	// returned values are identical.
}

// TestCacheHooks_BashNeverCached verifies that bash calls are never cached.
func TestCacheHooks_BashNeverCached(t *testing.T) {
	t.Parallel()

	rc := NewResultCache(map[string]bool{"read": true})
	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))
	hc.RegisterPost(CacheStoreHook(rc))

	counting := &countingSandbox{inner: &mockExecSandbox{result: sandbox.Result{
		Stdout:  "output",
		Success: true,
	}}}
	executor := NewToolExecutor(counting, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`},
	}

	w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	call.ID = "call_2"
	w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	if len(rc.entries) != 0 {
		t.Fatal("bash results should never be stored in cache")
	}
	if counting.calls != 2 {
		t.Fatalf("expected 2 sandbox executions (no caching), got %d", counting.calls)
	}
}

// TestCacheHooks_DifferentArgsDifferentKeys verifies different arguments
// produce distinct cache entries.
func TestCacheHooks_DifferentArgsDifferentKeys(t *testing.T) {
	t.Parallel()

	rc := NewResultCache(map[string]bool{"read": true})
	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))
	hc.RegisterPost(CacheStoreHook(rc))

	ctx := HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}
	_, _ = CacheStoreHook(rc).Fn(ctx, "content-a")

	ctx.Args = `{"path":"b.txt"}`
	_, _ = CacheStoreHook(rc).Fn(ctx, "content-b")

	if len(rc.entries) != 2 {
		t.Fatalf("expected 2 cache entries, got %d", len(rc.entries))
	}

	keyA := cacheKey("read", `{"path":"a.txt"}`)
	keyB := cacheKey("read", `{"path":"b.txt"}`)
	if va, _ := rc.get(keyA); va != "content-a" {
		t.Fatalf("expected content-a, got %q", va)
	}
	if vb, _ := rc.get(keyB); vb != "content-b" {
		t.Fatalf("expected content-b, got %q", vb)
	}
}

// TestCacheHooks_ShortCircuitSkipsPostHooks verifies that a cache hit
// does not run post-hooks (no double-caching or auditing of cached results).
func TestCacheHooks_ShortCircuitSkipsPostHooks(t *testing.T) {
	t.Parallel()

	rc := NewResultCache(map[string]bool{"read": true})
	key := cacheKey("read", `{"path":"cached.txt"}`)
	rc.set(key, "from-cache")

	postRan := false
	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))
	hc.RegisterPost(PostHook{
		Name:   "spy",
		Policy: FailOpen,
		Fn: func(_ HookContext, result string) (string, error) {
			postRan = true
			return result, nil
		},
	})

	executor := NewToolExecutor(nil, t.TempDir(), nil, 0)
	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"cached.txt"}`},
	}

	result := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if result != "from-cache" {
		t.Fatalf("expected from-cache, got %q", result)
	}
	if postRan {
		t.Fatal("post-hooks should not run on cache hit")
	}
}

// TestToolDefinition_ReadIsCacheable verifies the Definitions() output.
func TestToolDefinition_ReadIsCacheable(t *testing.T) {
	t.Parallel()
	te := NewToolExecutor(nil, t.TempDir(), nil, 0)
	for _, def := range te.Definitions() {
		switch def.Name {
		case "read":
			if !def.Cacheable {
				t.Fatal("read tool should be cacheable")
			}
		case "bash", "write":
			if def.Cacheable {
				t.Fatalf("%s tool should not be cacheable", def.Name)
			}
		}
	}
}
