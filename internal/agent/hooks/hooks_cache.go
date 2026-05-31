package hooks

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ResultCache is a per-session, concurrency-safe cache that maps
// (tool_name, canonicalized_args) → result. Only tools whose
// ToolDefinition has Cacheable == true are eligible for caching.
type ResultCache struct {
	mu        sync.RWMutex
	entries   map[string]string
	cacheable map[string]bool
}

// NewResultCache returns an empty ResultCache. The cacheableTools set
// determines which tool names are eligible for storage; tools not in the
// set are silently skipped.
func NewResultCache(cacheableTools map[string]bool) *ResultCache {
	if cacheableTools == nil {
		cacheableTools = map[string]bool{}
	}
	return &ResultCache{
		entries:   make(map[string]string),
		cacheable: cacheableTools,
	}
}

func (rc *ResultCache) isCacheable(tool string) bool {
	return rc.cacheable[tool]
}

func (rc *ResultCache) IsCacheable(tool string) bool {
	return rc.isCacheable(tool)
}

func (rc *ResultCache) get(key string) (string, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	v, ok := rc.entries[key]
	return v, ok
}

func (rc *ResultCache) Get(key string) (string, bool) {
	return rc.get(key)
}

func (rc *ResultCache) set(key, result string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries[key] = result
}

func (rc *ResultCache) Set(key, result string) {
	rc.set(key, result)
}

func (rc *ResultCache) EntryCount() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.entries)
}

// cacheKey produces a deterministic key from the tool name and a
// canonical JSON representation of the arguments.
func cacheKey(toolName, argsJSON string) string {
	canonical := canonicalizeArgs(argsJSON)
	h := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%s:%x", toolName, h)
}

func CacheKey(toolName, argsJSON string) string {
	return cacheKey(toolName, argsJSON)
}

// canonicalizeArgs re-serialises an arbitrary JSON object with sorted
// keys so that semantically identical arguments always hash identically.
func canonicalizeArgs(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return "{}"
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return trimmed
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		keyJSON, _ := json.Marshal(k)
		parts = append(parts, fmt.Sprintf("%s:%s", keyJSON, obj[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func CanonicalizeArgs(raw string) string {
	return canonicalizeArgs(raw)
}

// CacheLookupHook returns a PreHook that short-circuits tool execution
// when the cache already holds a result for the same (tool, args) pair.
// The cached result is returned as the veto reason so the dispatch layer
// can surface it directly.
func CacheLookupHook(cache *ResultCache) PreHook {
	return PreHook{
		Name:   "cache-lookup",
		Policy: FailOpen,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if cache == nil || !cache.isCacheable(ctx.ToolName) {
				return HookVerdict{}, nil
			}
			key := cacheKey(ctx.ToolName, ctx.Args)
			if result, ok := cache.get(key); ok {
				return HookVerdict{
					Veto:         true,
					ShortCircuit: true,
					Result:       result,
				}, nil
			}
			return HookVerdict{}, nil
		},
	}
}

// CacheStoreHook returns a PostHook that stores the tool result in the
// cache for future lookups. Only cacheable tools are stored.
func CacheStoreHook(cache *ResultCache) PostHook {
	return PostHook{
		Name:   "cache-store",
		Policy: FailOpen,
		Fn: func(ctx HookContext, result string) (string, error) {
			if cache == nil || !cache.isCacheable(ctx.ToolName) {
				return result, nil
			}
			key := cacheKey(ctx.ToolName, ctx.Args)
			cache.set(key, cacheStoredPayload(ctx, result))
			return result, nil
		},
	}
}

// cacheStoredPayload tags failed tool results with toolErrorPrefix before caching so
// cache hits classify the same as live execution. Successful reads may contain
// arbitrary JSON (including single-key {"error":...} fixtures) and are stored as-is.
func cacheStoredPayload(ctx HookContext, result string) string {
	if !ctx.ResultStatusSet || ctx.ResultStatus == ToolStatusSuccess {
		return result
	}
	if isToolErrorPayload(result) {
		return result
	}
	return toolErrorPrefix + result
}
