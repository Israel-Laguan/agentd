package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
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

	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))
	hc.RegisterPost(agenthooks.CacheStoreHook(rc))

	counting := &countingSandbox{inner: &mockExecSandbox{result: sandbox.Result{
		Stdout:  "hello world",
		Success: true,
	}}}
	executor := agenttools.NewToolExecutor(counting, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

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

	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))
	hc.RegisterPost(agenthooks.CacheStoreHook(rc))

	counting := &countingSandbox{inner: &mockExecSandbox{result: sandbox.Result{
		Stdout:  "output",
		Success: true,
	}}}
	executor := agenttools.NewToolExecutor(counting, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)

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

	if rc.EntryCount() != 0 {
		t.Fatal("bash results should never be stored in cache")
	}
	if counting.calls != 2 {
		t.Fatalf("expected 2 sandbox executions (no caching), got %d", counting.calls)
	}
}

func TestCacheHooks_DifferentArgsDifferentKeys(t *testing.T) {
	t.Parallel()

	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))
	hc.RegisterPost(agenthooks.CacheStoreHook(rc))

	ctx := agenthooks.HookContext{ToolName: "read", Args: `{"path":"a.txt"}`, Timestamp: time.Now()}
	_, _ = agenthooks.CacheStoreHook(rc).Fn(ctx, "content-a")

	ctx.Args = `{"path":"b.txt"}`
	_, _ = agenthooks.CacheStoreHook(rc).Fn(ctx, "content-b")

	if rc.EntryCount() != 2 {
		t.Fatalf("expected 2 cache entries, got %d", rc.EntryCount())
	}

	keyA := agenthooks.CacheKey("read", `{"path":"a.txt"}`)
	keyB := agenthooks.CacheKey("read", `{"path":"b.txt"}`)
	if va, _ := rc.Get(keyA); va != "content-a" {
		t.Fatalf("expected content-a, got %q", va)
	}
	if vb, _ := rc.Get(keyB); vb != "content-b" {
		t.Fatalf("expected content-b, got %q", vb)
	}
}

func TestCacheHooks_ShortCircuitSkipsPostHooks(t *testing.T) {
	t.Parallel()

	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	key := agenthooks.CacheKey("read", `{"path":"cached.txt"}`)
	rc.Set(key, "from-cache")

	postRan := false
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))
	hc.RegisterPost(agenthooks.PostHook{
		Name:   "spy",
		Policy: agenthooks.FailOpen,
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			postRan = true
			return result, nil
		},
	})

	executor := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_1",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"cached.txt"}`},
	}

	tr := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr.Status != agenttools.ToolStatusSuccess {
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

	cachedErr := agenttools.ToolErrorPrefix + `{"error":"stat failed: no such file"}`
	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	args := `{"path":"missing.txt"}`
	rc.Set(agenthooks.CacheKey("read", args), cachedErr)

	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))

	executor := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	w := &Worker{
		toolExecutor: executor,
		hooks:        hc,
	}

	call := gateway.ToolCall{
		ID:       "call_err",
		Function: gateway.ToolCallFunction{Name: "read", Arguments: args},
	}

	tr := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr.Status != agenttools.ToolStatusError {
		t.Fatalf("expected error status for cached error payload, got %s", tr.Status)
	}
	if tr.Content != agenttools.StripToolErrorPrefix(cachedErr) {
		t.Fatalf("content = %q, want %q", tr.Content, agenttools.StripToolErrorPrefix(cachedErr))
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

	rc := agenthooks.NewResultCache(map[string]bool{"read": true})
	hc := agenthooks.NewHookChain()
	hc.RegisterPre(agenthooks.CacheLookupHook(rc))
	hc.RegisterPost(agenthooks.CacheStoreHook(rc))

	executor := agenttools.NewToolExecutor(nil, dir, nil, 0)
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
	if tr1.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("first read Status = %s, want success", tr1.Status)
	}
	if tr1.ForContext() != content {
		t.Fatalf("first ForContext() = %q, want raw file content", tr1.ForContext())
	}

	call.ID = "call_2"
	tr2 := w.DispatchTool(context.Background(), "sess-1", call, nil, executor)
	if tr2.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("cached read Status = %s, want success", tr2.Status)
	}
	if tr2.ForContext() != content {
		t.Fatalf("cached ForContext() = %q, want raw file content without [ERROR]", tr2.ForContext())
	}
}
