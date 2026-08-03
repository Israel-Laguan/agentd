# Milestone 14 — Cache hygiene: stable prefix

**Status**: done · **PR scope**: one code PR · **Depends on**: M13 (positioning only —
not a hard build dependency). **Relates to**: strategy "Cache policy: keep & harden".

## Goal

Make first-request payloads **byte-stable and cache-ordered** so provider prompt caching
(DeepSeek automatic prefixes, OpenAI `cached_tokens`, LiteLLM passthrough) actually pays off.
Two small, high-leverage fixes plus a regression guard.

## Background / current state (verified)

- Memory lessons are appended after the stable system prompt via `appendMemoryLessons`
  (`internal/queue/worker/worker_messages.go:23-26`, called at `:208`, `:215`, and `:235`).
  There is no `prependMemoryLessons` in the repo; lessons now sit *after* the stable layered
  system prompt block, so the cache prefix for everything before them stays constant across tasks.
- Capability/plugin tool definitions are collected by iterating a Go map, but the result is now
  sorted canonically by name: `internal/capabilities/registry.go:63` and `internal/capabilities/registry.go:115`
  both call `sort.Strings(names)` before assembling `AIRequest.Tools`. The serialized `tools` block
  is therefore deterministic request-to-request.
- Core bash/read/write definitions are a fixed literal in
  `internal/agent/tools/tool_executor.go` `Definitions()` — already deterministic; leave as-is but
  keep it that way.
- Per-task `ToolManifest.Filter` (`internal/agent/tools/tool_manifest.go`) intentionally shrinks the
  tool set per task — acceptable within a task session, but it reduces cross-task prefix reuse.
  Keep the behavior; make its output order-stable; document the tradeoff.

## Scope (in)

1. Reorder memory lessons after the stable system prompt in `assembleAgenticSystemPrompt` /
   `assembleAgenticSystemPromptWithUserContent` (and the legacy `seedMessages` path if it shares
   the helper). Update any tests asserting message order (`worker_messages_test.go` and friends).
2. Ensure every `ToolDefinition` slice that reaches `AIRequest.Tools` is **deterministically
   ordered**:
   - Audit `Registry.GetTools` / `GetToolsAndAdapterIndex` (`capabilities/registry.go`), the
     subagent tool assembler, and the plugin loader; sort collected tools (e.g. by `Name`) where
     map-iteration order is used.
   - Make `ToolManifest.Filter`'s filtered output order-stable.
3. Add a **golden byte-stability test**: seed the same task (same project/profile/task, no memory
   lessons or with fixed lessons) twice and assert the serialized first gateway request
   (system + tools + messages) is byte-identical across runs. This guards against future regressions
   (timestamps, map-order, injected randomness).
4. Document the cache-breaking points in code comments and (if not already) in the strategy doc:
   truncation collapse, topic-drift rewind (`agentic/rewind.go`), targeted redo
   (`message_editor`, `corrections`). These are deliberate compaction, not bugs.

## Scope (out)

- No change to which tools exist or whether per-task filtering happens.
- No cache-metrics plumbing (that is M15).
- No change to provider wire format.

## Files to touch (likely; final set via search)

- `internal/queue/worker/worker_messages.go`
- `internal/capabilities/registry.go`
- `internal/agent/subagent/subagent_tools.go`
- `internal/capabilities/plugin/loader.go`
- `internal/agent/tools/tool_manifest.go`
- New golden-stability test under `internal/queue/worker/` and/or `internal/gateway/`

## Verification

- `go build ./...` clean.
- `go test ./internal/queue/worker/... ./internal/capabilities/... ./internal/agent/...` green;
  new golden byte-stability test passes.
- Confirm no behavioral change beyond message/tool ordering (existing agentic worker and routing
  suites still pass with `--runInBand` where required).
