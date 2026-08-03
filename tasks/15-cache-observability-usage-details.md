# Milestone 15 — Cache observability: usage details

**Status**: not started · **PR scope**: one code PR · **Depends on**: M14 (measures what M14
fixes — can be authored in parallel but merged after). **Relates to**: strategy "Cache
observability: keep & harden".

## Goal

Surface prompt-cache hit rates so operators can verify the M14 wins and monitor effective cost.
Today `AIResponse` carries only a single `TokenUsage int`, and the openai adapter parses only
`usage.total_tokens` — every provider cache-token field is dropped on the floor.

## Background / current state (verified)

- `spec.AIResponse` (`internal/gateway/spec/spec.go:110-116`) has only:
  `Content, TokenUsage int, ProviderUsed, ModelUsed, ToolCalls`.
- openai adapter (`internal/gateway/providers/openai.go:204-254`): `Usage struct { TotalTokens,
  PromptTokens, CompletionTokens }`; only `TotalTokens` is copied into `TokenUsage`.
- Worker `TOKEN_USAGE` event path: `TokenUsageStore.AddTokenUsage(taskID, tokens int)`
  (see `worker_token_usage_test.go` for wiring). Cache fields would need to ride along.

## Scope (in)

1. Extend `spec.AIResponse` with cache fields (keep `TokenUsage` for compatibility), e.g.:
   ```go
   type UsageDetails struct {
       CachedTokens    int `json:"cached_tokens,omitempty"`       // prompt cache reads
       CacheWriteTokens int `json:"cache_write_tokens,omitempty"` // prompt cache writes
   }
   AIResponse { ...; UsageDetails UsageDetails `json:"usage_details,omitempty"` }
   ```
2. Parse in the openai adapter:
   - `usage.prompt_tokens_details.cached_tokens` (OpenAI and LiteLLM passthrough).
   - `usage.prompt_cache_hit_tokens` / `usage.prompt_cache_miss_tokens` (DeepSeek via
     OpenAI-compatible format).
   - Gracefully tolerate absence (many providers omit these).
3. Surface the fields:
   - Worker `TOKEN_USAGE` event payload (extend the event payload struct and the
     `TokenUsageStore` interface with an additive method or a richer payload — follow existing
     patterns; do not break existing callers).
   - Debug logs alongside `TokenUsage`.
4. Tests: provider fixture tests that supply usage-details JSON and assert the parsed fields;
   a worker token-usage test asserting the new fields land in the event payload.

## Scope (out)

- No Anthropic `cache_read/creation_input_tokens` in the frozen native adapter (optional stretch,
  only if cheap and low-risk).
- No dashboards / no hit-rate math in the daemon — just expose the raw fields.
- No change to wire request format.

## Files to touch (likely; final set via search)

- `internal/gateway/spec/spec.go`
- `internal/gateway/providers/openai.go` (+ `openai_test.go`)
- `internal/queue/worker/*token_usage*` / token-usage store + event payload
- Corresponding test files

## Verification

- `go build ./... && go test ./internal/gateway/... ./internal/queue/worker/...` green.
- New fixture tests confirm cached-token parsing for OpenAI and DeepSeek shapes.
