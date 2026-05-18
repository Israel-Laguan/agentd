package worker

import (
	"testing"
	"time"
)

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

func TestCacheStoreHook_PrefixesNonSuccessResult(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheStoreHook(rc)

	ctx := HookContext{
		ToolName:        "read",
		Args:            `{"path":"missing.txt"}`,
		Timestamp:       time.Now(),
		ResultStatus:    ToolStatusError,
		ResultStatusSet: true,
	}
	payload := `{"error":"stat failed: no such file"}`
	_, err := hook.Fn(ctx, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	key := cacheKey("read", `{"path":"missing.txt"}`)
	cached, ok := rc.get(key)
	if !ok {
		t.Fatal("expected cached result")
	}
	if !isToolErrorPayload(cached) {
		t.Fatalf("cached error should be prefixed, got %q", cached)
	}
	if stripToolErrorPrefix(cached) != payload {
		t.Fatalf("cached = %q, want prefixed %q", cached, payload)
	}
}

func TestCacheStoreHook_DoesNotPrefixSuccessResult(t *testing.T) {
	t.Parallel()
	rc := NewResultCache(map[string]bool{"read": true})
	hook := CacheStoreHook(rc)

	content := `{"error":"cached api failure"}`
	ctx := HookContext{
		ToolName:        "read",
		Args:            `{"path":"response.json"}`,
		Timestamp:       time.Now(),
		ResultStatus:    ToolStatusSuccess,
		ResultStatusSet: true,
	}
	_, err := hook.Fn(ctx, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	key := cacheKey("read", `{"path":"response.json"}`)
	cached, ok := rc.get(key)
	if !ok || cached != content {
		t.Fatalf("success payload stored as-is, got (%q, %v)", cached, ok)
	}
}

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
