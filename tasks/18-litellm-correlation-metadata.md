# Milestone 18 — LiteLLM correlation metadata + topology log (optional)

**Status**: not started (optional) · **PR scope**: one code PR · **Depends on**: M13 (recipe/M15
fields). **Relates to**: strategy "spend/observability delegated to connector, but correlatable".

## Goal

Attribute proxy-side spend and request logs to agentd tasks/agents, and make the effective
connector topology visible at startup.

## Background / current state (verified)

- Provider entries accept adapter-specific `options` (`spec.ProviderConfig.Options`, per
  `config-reference.md`); each adapter reads only known keys.
- The openai adapter builds its body via `openaiRequest`; it currently has no per-request metadata
  channel for correlation.
- Startup currently logs provider capability lines (`provider supports chat tools`) but not an
  explicit topology summary.

## Scope (in)

1. **Correlation metadata (opt-in):** add an openai-adapter option, e.g.
   `options: { send_task_metadata: true }`, that causes the adapter to include a stable
   attribution field on the request body when the request carries it. Concretely:
   - Plumb `AIRequest.TaskID` / `AgentID` (and optionally `Provider` role) into a LiteLLM-friendly
     field. Prefer `user` (a stable string OpenAI/LiteLLM accept) or a `metadata` object
     (LiteLLM records `metadata`); pick the one that survives passthrough, and gate it so it is NOT
     sent unless the option is true (avoid leaking task ids to providers that don't want them).
   - Add a fixture test asserting the field appears in the request body when enabled and is absent
     when disabled.
2. **Topology log:** at startup, log one line summarizing the effective topology per role
   (chat/worker/memory): e.g. `connector topology: worker=managed(litellm) chat=direct(openai)`,
   derived from which provider each role routes to and whether that entry points at a proxy base_url
   (or simply logs the provider/model names). Keep it a log-only addition.

## Scope (out)

- No change to default request bodies (metadata is opt-in).
- No spend aggregation in agentd (that is LiteLLM's job).

## Files to touch (likely; final set via search)

- `internal/gateway/providers/openai.go` (+ test)
- `internal/gateway/spec/spec.go` (only if `AIRequest` needs a reusable metadata field)
- `cmd/agentd/start.go` or `internal/config` startup wiring for the topology log
- docs (recipe/`options` table)

## Verification

- `go build ./... && go test ./internal/gateway/...` green.
- Fixture test: metadata present when option on, absent when off.
- Manual startup log check shows the topology line.
