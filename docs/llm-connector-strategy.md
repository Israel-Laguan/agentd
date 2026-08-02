# LLM connector strategy

This document declares agentd's **two-topology connector model** and the boundary
that decides whether a piece of LLM-related behavior lives in agentd or in an
external proxy. It is docs-only (Milestone 13); no Go code changes are expected.
It **unblocks positioning** for all later milestones (M14–M19), which own the
cache, observability, wire-contract, and provider-recipe work this doc defers.

See also: [`provider-tool-calling.md`](provider-tool-calling.md) (capability
matrix), [`openai-compatible-providers.md`](openai-compatible-providers.md)
(named vendor + LiteLLM recipes), [`agentic-harness-roadmap.md`](agentic-harness-roadmap.md)
(phased roadmap + status).

## The two topologies

### Direct path — zero dependencies, always works

```
agentd ──HTTP──▶ llama.cpp / OpenAI / vLLM / Ollama / Gemini(v1) / LiteLLM
                     via adapter: openai (OpenAI Chat Completions wire format)
```

`adapter: openai` is the hardened wire path (Phases 1–6 done, see
`agentic-harness-roadmap.md`). Every topology rides it. For a single provider or
a local server you can point a `gateway.providers` entry (or the built-in
`openai` slot) at any OpenAI-compatible endpoint — this is the fastest way to
agentic mode and needs nothing running but agentd itself.

### Managed path — recommended for multi-provider / production

```
agentd ──HTTP──▶ LiteLLM (or Portkey / OpenRouter) ──▶ N providers
                (OpenAI Chat Completions at /v1/chat/completions)
```

agentd keeps **agent-side semantics** (tools, truncation, budgets, cascade,
circuit breaker) and the proxy owns everything else (keys, retries, rate limits,
budgets, caching, observability, wire-format translation). You get provider
diversity, virtual keys, and Anthropic-native format translation for free, because
the proxy speaks OpenAI-compatible Chat Completions back to agentd.

The boundary is sharp: if you want something a provider *does*, keep it in
agentd. If you want something a provider *account* does, delegate it.

## The boundary rule

> **If a feature needs to know about tasks, agents, messages, or tools → keep in agentd.
> If it needs to know about API keys, provider accounts, quotas, or billing → delegate to the connector.**

Rationale: agentd's value is agent *behavior* — how tasks decompose, how history
truncates, how budgets and circuit breakers gate retries, how roles route. None of
that is specific to OpenAI vs. Anthropic vs. llama.cpp, so it must not be encoded in
per-provider adapters. Provider-account surface area (credentials, per-model quotas,
billing) instead varies by deployment and is a distraction from the agent contract.

## Keep / delegate table

| Feature | Decision | Note |
| --- | --- | --- |
| Wire contract types (`spec/spec.go`: messages, tools, tool_calls) | Keep & harden | Agent-side contract; consolidated in M16 |
| OpenAI-compatible adapter (incl. LiteLLM) | Keep & harden — THE ONE wire path | Both topologies ride it |
| llama.cpp direct adapter | Keep, frozen | Zero-dep promise; runtime-capability gating in M17 |
| Native anthropic / ollama / horde adapters | Freeze → maintenance mode | Bug-fix only; tool path is the proxy (see below) |
| New provider intake | Delegate | No new native adapters |
| Key management / virtual keys / rotation | Delegate | LiteLLM master key + virtual keys |
| Per-account spend / budgets | Delegate | LiteLLM budgets |
| Per-`task` token budget | Keep & harden | `internal/gateway/budget.go`, task-scoped |
| Provider rate limits (RPM) | Delegate | agentd keeps its concurrency semaphore |
| Load balancing / upstream retries | Delegate | LiteLLM router; agentd cascade stays 1-attempt/candidate |
| Cascade fallback + circuit breaker | Keep | `internal/gateway/routing/router_cascade.go`, `internal/queue/safety/breaker.go` |
| Role routing (chat/worker/memory) | Keep & harden | `internal/gateway/routing/router.go` `WithRoleRouting`, `role_models` |
| Agentic tool-history truncation (pairwise consistency) | Keep & harden | Crown jewel (`internal/gateway/truncation/`) |
| Legacy JSON mode + repair | Keep & harden | `internal/gateway/routing/router.go` + `internal/gateway/correction/` |
| House rules / intent / scope / plan workflows | Keep | Agent behaviors |
| Cache **policy** (prefix ordering, stable tools) | Keep & harden (M14) | Proxies can't fix a badly ordered prompt |
| Cache **mechanics** (`cache_control` breakpoints) | Delegate | LiteLLM / Portkey inject Anthropic breakpoints |
| Cache observability (hit rate) | Keep & harden (M15) | Usage-details parsing + `TOKEN_USAGE` events |
| Streaming / vision / `image_url` / Responses API / built-in tools | Deferred non-goal | Proxy or direct provider client |
| Embeddings | Keep | Memory subsystem; also rides LiteLLM |
## Provider status (corrected)

Native adapters that report `SupportsChatTools: true` but are **not** reliable for
the agentic inner loop are now in **maintenance mode** (bug-fix only). The
multi-turn tool round-trip is owned by the direct-managed paths above; see
[`provider-tool-calling.md`](provider-tool-calling.md) for the corrected matrix.

- **OpenAI** (`adapter: openai`): verified multi-turn. The single hardened path.
- **Anthropic** (native): **single-turn tools only** — multi-turn agentic requires the
  proxy path. Two concrete defects in `internal/gateway/providers/anthropic.go`:
  1. `anthropicContentBlock` expects a *nested* `tool_use` field, but Anthropic's
     Messages API returns *flat* `tool_use` blocks (`{"type":"tool_use","id":..,"name":..,"input":{..}}`),
     so `ToolUse` is never populated from a real response.
  2. `splitSystemMessages` flattens every `PromptMessage` to `{role, content}`,
     dropping assistant `ToolCalls` and tool-role `ToolCallID`. Anthropic requires
     `tool_result` content blocks inside the prior `user` message and matching
     `tool_use` blocks in the assistant message — neither round-trips today.
  Fixture tests in `anthropic_test.go` marshal the app's own structs, so they are
  self-referential and do not exercise the real wire format.
- **Ollama** (native): `SupportsChatTools: false`; maintenance mode. `/api/chat` supports
  a `tools` field, but the proxy path (or a direct OpenAI-compatible client) is the
  supported route for agentic mode.
- **Horde** (native): `SupportsChatTools: false`; maintenance mode. Async text-only,
  never tool-capable.
- **Gemini**: openai-compatible endpoint (`adapter: openai`); verified.
- **llama.cpp**: `adapter: openai`-style `/v1/chat/completions`; capability gating is
  runtime-dependent (M17). OpenAI-compatible, so it rides the hardened path.

## Non-goals

These are intentionally **out of scope** for agentd's own adapters. If you need
them, use a proxy (LiteLLM / Portkey / OpenRouter) or a direct provider client;
do **not** open an issue asking agentd to implement them:

- **Streaming** (`stream: true` / SSE `delta` streaming).
- **Vision / `image_url`** content blocks.
- **OpenAI Responses API** (`previous_response_id`, Conversations).
- **Anthropic-native** Messages wire format (use the proxy or the openai-compatible
  adapter).
- **Provider built-in tools** (code interpreters, file search, etc.) — these are
  provider-controlled, not agentd behaviors.

## Cache discipline

Prompt caching is a *policy* concern (agentd decides where cache breakpoints make
sense), but a *mechanics* concern (LiteLLM / Portkey inject `cache_control`
breakpoints). Agentd owns the policy so the prefix it hands the proxy is stable.

- **Stable-prefix ordering** for the assembled prompt:
  1. layered system prompt,
  2. tool definitions (sorted by name),
  3. stable context (task seed / intent — never mutates mid-session),
  4. memory lessons (appended after the task seed so the system-prompt + tool-defs
     prefix is identical across tasks with the same profile/task),
  5. append-only tool-call / tool-result history (the only mutable tail).
- **Determinism**: no timestamps, UUIDs, or wall-clock references in prompts or
  tool schemas; tool-definition ordering is sorted, not map-iterated; tool schemas
  omit volatile fields.
- **Deliberate-compaction list** (each is a *cache-breaking event* → M14 golden
  test and M15 observability own enforcement/reporting):
  - **Truncation collapse** — history re-packing changes prompt bytes.
  - **Topic-drift rewind** — a lesson is re-summarized or removed, shifting prefix.
  - **Targeted redo** — a tool call is re-issued from scratch (new call id).

## LiteLLM recipe (worked)

The managed path is a single `gateway.providers` entry pointing at a local
LiteLLM (or Portkey / OpenRouter) server. See
[`openai-compatible-providers.md`](openai-compatible-providers.md#litellm-managed-proxy)
for the full worked config, the Podman `host.containers.internal` DNS caveat,
one-entry-per-model-alias guidance, and retry-amplification notes.

```yaml
gateway:
  providers:
    - name: litellm
      adapter: openai
      base_url: "http://127.0.0.1:4000/v1"
      api_key_env: LITELLM_API_KEY   # LiteLLM master key
      model: "poolside/laguna-m.1"   # LiteLLM model_name alias
      capabilities: { chat_tools: true }
  order: [litellm]
```

## Files

This milestone is docs + config only. No `internal/` Go is touched; the native
adapters are **not** deleted or rewritten — only their documented status is
corrected.

- `docs/llm-connector-strategy.md` (this file)
- `docs/provider-tool-calling.md`
- `docs/openai-compatible-providers.md`
- `docs/agentic-harness.md`
- `docs/agentic-harness-roadmap.md`
- `config.reference.yaml`
- `.env.example`
- `CONTRIBUTING.md`

## Verification

- Docs-only PR: `git diff --stat` shows only docs/config changes.
- `go build ./... && go test ./docs/...` — existing tests pass (link validation +
  provider-tool-calling parity test).
- Link check: a `grep` for backtick-relative `tasks/` references in `docs/` must point
  only at task files that exist under `tasks/` (`13`–`19` all exist).
- Reviewer confirms the provider claims (Anthropic "single-turn", ollama/horde
  "maintenance") match the code evidence cited above.