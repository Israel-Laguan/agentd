# Milestone 13 — LLM connector strategy (docs-only)

**Status**: not started · **PR scope**: one docs-only PR · **Depends on**: nothing.
**Relates to**: roadmap tasks 09, 12 (re-scoped), and the strategy decision to delegate provider
diversity to an external proxy (LiteLLM / Portkey / OpenRouter or equivalent).

## Goal

Declare and document the **two-topology** LLM connector model and correct provider claims that
are stale or wrong today. Zero code changes are expected in this milestone; it unblocks the
positioning for all later milestones.

- **Direct path** (zero-dependency, always works): `agentd → llama.cpp / OpenAI / vLLM / Ollama /v1`
  via the `adapter: openai` (OpenAI Chat Completions) wire format.
- **Managed path** (recommended for multi-provider / production): `agentd → LiteLLM (or Portkey /
  OpenRouter) → N providers`. agentd keeps agent-side semantics; the proxy owns keys, retries,
  rate limits, budgets, caching mechanics, observability, and provider wire-format translation.

## Background / current state (verify by reading, then update docs)

- The openai-compatible adapter is the hardened wire path (Phases 1–3) and is the natural home
  for both topologies. See `docs/openai-compatible-providers.md`.
- Native `anthropic` adapter reports `SupportsChatTools: true` and is documented as "Verified" in
  `docs/provider-tool-calling.md`, but its **multi-turn tool round-trip is broken**:
  - Response parsing expects a *nested* `tool_use` key (`internal/gateway/providers/anthropic.go`
    `anthropicContentBlock`), but Anthropic's Messages API returns *flat* `tool_use` blocks
    (`{"type":"tool_use","id":..,"name":..,"input":{..}}`) — fixtures in `anthropic_test.go`
    marshal the app's own structs, so tests are self-referential.
  - `splitSystemMessages` maps every `PromptMessage` to `{role, content}` strings and drops
    assistant `ToolCalls` and tool-role `ToolCallID`; Anthropic requires `tool_result` content
    blocks inside a `user` message and matching `tool_use` blocks in the prior assistant message.
- Native `ollama` sends no `tools` and parses no `tool_calls` (`SupportsChatTools: false`); native
  `horde` is async-text-only (never tool-capable).
- `docs/agentic-harness-roadmap.md` links to `tasks/01`–`12` files that never existed; the
  `../tasks/` link in `docs/agentic-harness.md` is therefore broken too.

## Scope (in)

1. New `docs/llm-connector-strategy.md` containing:
   - The boundary rule: *"If a feature needs to know about tasks, agents, messages, or tools →
     keep in agentd. If it needs to know about API keys, provider accounts, quotas, or billing →
     delegate to the connector."*
   - The keep / delegate table (canonical rows below).
   - Explicit **non-goals**: streaming, vision/`image_url`, OpenAI Responses API
     (`previous_response_id`, Conversations), Anthropic-native message format, provider built-in
     tools → "use a proxy (LiteLLM / Portkey / OpenRouter) or a direct provider client".
   - A **Cache discipline** section: stable-prefix ordering (layered system prompt → tool defs →
     stable context → memory lessons → append-only history), no timestamps/UUIDs in prompts or
     tool schemas, and the deliberate-compaction list (truncation collapse, topic-drift rewind,
     targeted redo) documented as cache-breaking events.
2. Correct `docs/provider-tool-calling.md`:
   - Anthropic row → **"single-turn tools only — multi-turn agentic requires the proxy path"**, with
     the two concrete defects above cited; add a maintenance-mode banner.
   - Add maintenance-mode banners to native `ollama` and `horde` rows.
3. LiteLLM recipe: a worked config in `docs/openai-compatible-providers.md` (or the strategy doc):
   ```yaml
   gateway:
     providers:
       - name: litellm
         adapter: openai
         base_url: "http://127.0.0.1:4000/v1"
         api_key_env: LITELLM_API_KEY      # = LiteLLM master_key
         model: "poolside/laguna-m.1"      # LiteLLM model_name alias
         capabilities: { chat_tools: true }
     order: [litellm]
   ```
   Plus: `host.containers.internal` Podman DNS caveat; one entry per model alias; and
   **retry-amplification guidance** (set LiteLLM `num_retries: 0–1` because agentd's
   `router_cascade.go` already does 1-attempt-per-candidate fallback).
4. `config.reference.yaml`: add a commented LiteLLM example under `gateway.providers`.
5. `.env.example`: add `LITELLM_API_KEY=`.
6. `CONTRIBUTING.md`: add a rule — *"No new native provider adapters. New providers are added as
   LiteLLM model aliases or openai-compatible `gateway.providers` entries."*
7. Milestone 13 also owns **M0 bookkeeping** (see the companion roadmap update): add a status column
   to `docs/agentic-harness-roadmap.md`, mark tasks 08–11 done / 12 re-scoped, and link the new
   `tasks/13`–`19` files; fix the stale "no first-class tool-call events" line in
   `docs/agentic-harness.md`.

## Canonical keep / delegate table (for the strategy doc)

| Feature | Decision | Note |
| --- | --- | --- |
| Wire contract types (`spec.go`: messages, tools, tool_calls) | Keep & harden | Agent-side contract; consolidated in M16 |
| OpenAI-compatible adapter (incl. LiteLLM) | Keep & harden — the ONE wire path | Both topologies ride it |
| llama.cpp direct adapter | Keep, frozen | Zero-dep promise; runtime-capability gating in M17 |
| Native anthropic / ollama / horde adapters | Freeze → maintenance mode | Bug-fix only; tool path is the proxy |
| New provider intake | Delegate | No new native adapters |
| Key management / virtual keys / rotation | Delegate | LiteLLM master key + virtual keys |
| Per-account spend / budgets | Delegate | LiteLLM budgets |
| Per-`task` token budget | Keep & harden | `gateway/budget.go`, task-scoped |
| Provider rate limits (RPM) | Delegate | agentd keeps its concurrency semaphore |
| Load balancing / upstream retries | Delegate | LiteLLM router; agentd cascade stays 1-attempt/candidate |
| Cascade fallback + circuit breaker | Keep | `router_cascade.go`, `queue/safety/breaker.go` |
| Role routing (chat/worker/memory) | Keep & harden | `router.go` `WithRoleRouting`, `role_models` |
| Agentic tool-history truncation (pairwise consistency) | Keep & harden | Crown jewel (`gateway/truncation/`) |
| Legacy JSON mode + repair | Keep & harden | `routing/router.go` + `correction/` |
| House rules / intent / scope / plan workflows | Keep | Agent behaviors |
| Cache **policy** (prefix ordering, stable tools) | Keep & harden (M14) | Proxies can't fix a badly ordered prompt |
| Cache **mechanics** (`cache_control` breakpoints) | Delegate | LiteLLM / Portkey inject Anthropic breakpoints |
| Cache observability (hit rate) | Keep & harden (M15) | Usage-details parsing + TOKEN_USAGE events |
| Streaming / vision / Responses API / built-in tools | Deferred non-goal | Proxy or direct provider client |
| Embeddings | Keep | Memory subsystem; also rides LiteLLM |
| Secrets scrubbing before egress / content guardrails | Keep | Data protection must precede proxy egress |

## Scope (out)

- No Go code changes (M14/M15/M16 own the code).
- No changes to `internal/queue/safety/breaker.go` or cascade semantics.
- Not deleting or rewriting native adapters — only correcting their documented status.

## Files to touch

- `docs/llm-connector-strategy.md` (new)
- `docs/provider-tool-calling.md`
- `docs/openai-compatible-providers.md`
- `docs/agentic-harness-roadmap.md`
- `docs/agentic-harness.md`
- `config.reference.yaml`
- `.env.example`
- `CONTRIBUTING.md`

## Verification

- Docs-only PR: no build impact (`git diff --stat` shows only docs/config).
- Existing tests still pass (`go build ./... && go test ./...`) since no Go changed.
- Link check: `grep -rn "](../tasks/" docs/` must reference only files that exist under `tasks/`.
- Reviewer confirms provider claims (Anthropic "single-turn", ollama/horde "maintenance") match
  the code evidence cited above.

