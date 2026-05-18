package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// countingSandbox wraps mockExecSandbox and counts Execute calls.
type countingSandbox struct {
	inner sandbox.Executor
	calls int
}

func (c *countingSandbox) Execute(ctx context.Context, p sandbox.Payload) (sandbox.Result, error) {
	c.calls++
	return c.inner.Execute(ctx, p)
}

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

	tr1 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	call.ID = "call_2"
	tr2 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	if tr1.Content != tr2.Content {
		t.Fatalf("expected identical results:\n  first:  %q\n  second: %q", tr1.Content, tr2.Content)
	}
}

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

	_ = w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	call.ID = "call_2"
	_ = w.DispatchTool(context.Background(), "sess-1", call, nil, executor)

	if len(rc.entries) != 0 {
		t.Fatal("bash results should never be stored in cache")
	}
	if counting.calls != 2 {
		t.Fatalf("expected 2 sandbox executions (no caching), got %d", counting.calls)
	}
}

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

	tr := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr.Status != ToolStatusSuccess {
		t.Fatalf("expected success status, got %s", tr.Status)
	}
	if tr.Content != "from-cache" {
		t.Fatalf("expected from-cache, got %q", tr.Content)
	}
	if postRan {
		t.Fatal("post-hooks should not run on cache hit")
	}
}

func TestCacheHooks_CachedReadErrorClassified(t *testing.T) {
	t.Parallel()

	cachedErr := toolErrorPrefix + `{"error":"stat failed: no such file"}`
	rc := NewResultCache(map[string]bool{"read": true})
	args := `{"path":"missing.txt"}`
	rc.set(cacheKey("read", args), cachedErr)

	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))

	executor := NewToolExecutor(nil, t.TempDir(), nil, 0)
	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_err",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: args},
	}

	tr := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr.Status != ToolStatusError {
		t.Fatalf("expected error status for cached error payload, got %s", tr.Status)
	}
	if tr.Content != stripToolErrorPrefix(cachedErr) {
		t.Fatalf("content = %q, want %q", tr.Content, stripToolErrorPrefix(cachedErr))
	}
}

func TestCacheHooks_CachedReadErrorShapedFileContentIsSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `{"error":"cached api failure"}`
	path := filepath.Join(dir, "response.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rc := NewResultCache(map[string]bool{"read": true})
	hc := NewHookChain()
	hc.RegisterPre(CacheLookupHook(rc))
	hc.RegisterPost(CacheStoreHook(rc))

	executor := NewToolExecutor(nil, dir, nil, 0)
	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	args := `{"path":"response.json"}`
	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: args},
	}

	tr1 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr1.Status != ToolStatusSuccess {
		t.Fatalf("first read Status = %s, want success", tr1.Status)
	}
	if tr1.ForContext() != content {
		t.Fatalf("first ForContext() = %q, want raw file content", tr1.ForContext())
	}

	call.ID = "call_2"
	tr2 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr2.Status != ToolStatusSuccess {
		t.Fatalf("cached read Status = %s, want success", tr2.Status)
	}
	if tr2.ForContext() != content {
		t.Fatalf("cached ForContext() = %q, want raw file content without [ERROR]", tr2.ForContext())
	}
}
