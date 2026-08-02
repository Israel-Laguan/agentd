# Provider Tool Calling

agentd keeps provider tool support conservative. A provider must keep
`SupportsChatTools` false until it has request mapping, response parsing, and
fixture tests for that provider's actual wire format.

Each backend implements `Backend.Capabilities()` in
[`internal/gateway/providers/provider.go`](../internal/gateway/providers/provider.go),
returning `Capabilities{SupportsChatTools: ...}`. Adapter defaults can be overridden
per entry via `capabilities.chat_tools` in config.

The **router** (`*routing.Router`) is the single source of truth for per-provider
`SupportsChatTools` via [`ProviderSupportsChatTools`](../internal/gateway/routing/router.go).
The worker gate calls `gateway.ProviderSupportsChatTools(gw, name)`, which delegates to
the router's backend registry; there is no separate static string switch. At startup,
agentd logs `provider supports chat tools` for every provider in the order list whose
backend returns `SupportsChatTools: true`.

## Capability Matrix

| Provider | `SupportsChatTools` | Status | Notes |
| --- | --- | --- | --- |
| OpenAI | `true` | Verified | Sends OpenAI-compatible `tools` and parses `tool_calls` in provider fixture tests. The hardened single wire path. |
| Anthropic | `true` | ⚠️ Maintenance (single-turn) | Single-turn tools only; **multi-turn agentic requires the proxy path** (`adapter: openai` via LiteLLM / Portkey / OpenRouter). See §[Provider Deltas — Anthropic](#anthropic) for the two defects; native fixture tests are self-referential. Bug-fix only. |
| Gemini | `true` | Verified | OpenAI-compatible endpoint via `name: gemini`, `adapter: openai` (legacy `adapter: gemini` alias is accepted). Sends `tools` and parses `tool_calls` like OpenAI. |
| Ollama | `false` | ⚠️ Maintenance | `/api/chat` supports a `tools` field and returns `message.tool_calls`, but support depends on server and model behavior. Native adapter is bug-fix only; use the proxy path for agentic tools. |
| llama.cpp | `false` | Frozen (openai-compatible) | OpenAI-style function calling depends on runtime setup such as `llama-server --jinja`, chat templates, and model support; runtime capability gating is planned (M17). Uses `adapter: openai` when available, so it rides the hardened path. |
| AI Horde | `false` | ⚠️ Maintenance | The current provider uses async text generation with prompt and Kobold-style generation parameters, not a chat tool-call contract. Native adapter is bug-fix only; not tool-capable. |

> ⚠️ **Maintenance-mode providers** — native `anthropic`, `ollama`, and `horde` adapters
> are **bug-fix only** as of Milestone 13 (see [LLM connector strategy](llm-connector-strategy.md#provider-status-corrected)).
> They are retained for the zero-dependency promise and direct-path use, but **do not**
> receive new features or tool-path fixes. For agentic tool calling, add a `gateway.providers`
> entry with `adapter: openai` pointing at the OpenAI-compatible endpoint exposed by a
> managed proxy (LiteLLM / Portkey / OpenRouter) or the provider directly.

## Provider Deltas

### Anthropic

> ⚠️ **Maintenance mode (single-turn tools only).** The native `anthropic` adapter is
> bug-fix only. The agentic inner loop needs multi-turn tool round-tripping, which is
> **broken** today — use the proxy path (`adapter: openai` via LiteLLM / Portkey /
> OpenRouter) for agentic tool calling. See [LLM connector strategy](llm-connector-strategy.md#provider-status-corrected).

The adapter maps `AIRequest.Tools` to Messages API `tools` (`name`, `description`,
`input_schema`) and parses response `content` blocks with `type: "tool_use"` into
`AIResponse.ToolCalls`, serializing the block `input` object into
`ToolCallFunction.Arguments`; text blocks remain normal response content. This works
for a **single** request/response turn only. The multi-turn loop is broken in two
concrete places (`internal/gateway/providers/anthropic.go`):

1. **Response parsing** — `anthropicContentBlock` expects a *nested* `tool_use` object
   (field `ToolUse` with `json:"tool_use"`), but Anthropic's Messages API returns
   *flat* `tool_use` blocks (`{"type":"tool_use","id":"...","name":"...","input":{...}}`).
   On a real response the nested field is never populated, so `ToolCalls` ends up empty.
2. **Request building** — `splitSystemMessages` flattens every `PromptMessage` to
   `{role, content}`, dropping assistant `ToolCalls` and the tool-role `ToolCallID`.
   Anthropic requires `tool_result` content blocks inside the prior `user` message and
   matching `tool_use` blocks in the assistant message, so a second turn can never carry
   the prior tool result back.

Fixture tests in `anthropic_test.go` marshal the app's own `anthropicResponse` struct
(producing nested JSON), so they are **self-referential** and do not catch defect #1.
Fixing the round-trip is out of scope for this docs-only milestone; until then the
managed proxy path is the supported route for agentic tool calling.

### Ollama

> ⚠️ **Maintenance mode.** The native `ollama` adapter is bug-fix only. `/api/chat`
> supports a `tools` field, but agentic tool calling is not validated against this
> adapter — use the proxy path (`adapter: openai` via LiteLLM / Portkey / OpenRouter)
> for agentic tool calling. See [LLM connector strategy](llm-connector-strategy.md#provider-status-corrected).

The current provider uses native `/api/chat`, not OpenAI-compatible
`/v1/chat/completions`. Native chat accepts OpenAI-like `tools` objects with
`type: "function"` and `function.parameters`.

Parse `message.tool_calls[].function.name` and object `arguments`; synthesize
stable call IDs if Ollama omits them. Keep `SupportsChatTools` false until
fixture tests cover passthrough, parsing, and version/model gating.

### llama.cpp

The provider targets `/v1/chat/completions`, but function calling is only a
valid claim for known-compatible server startup and model/template combinations.
Runtime capability detection should come before flipping the provider flag.

OpenAI-compatible responses can include `tool_calls`; some templates and generic
handlers may have partial behavior, and parallel calls are opt-in. Keep
`SupportsChatTools` false until fixture tests cover a known compatible setup.

### AI Horde

> ⚠️ **Maintenance mode.** The native `horde` adapter is bug-fix only and async
> text-only; it is not tool-capable. Use the proxy path for agentic tool calling.
> See [LLM connector strategy](llm-connector-strategy.md#provider-status-corrected).

The provider targets `/v2/generate/text/async`. The documented request model is
prompt plus generation parameters, and status returns generated text rather than
structured chat messages or tool-call objects.

Keep `SupportsChatTools` false unless Horde adds a tool-call-capable API or this
provider switches to a tested proxy with an explicit contract.

## AgenticMode Gate

`SupportsChatTools` controls two independent gates:

**Path A — Router (frontdesk / single-shot tool calls):** When a client sends `tools` on
`POST /v1/chat/completions`, the router checks `ProviderSupportsChatTools` to decide whether
to forward tool definitions to the provider. This is the only gate for the Frontdesk planning
flow and for single-shot worker requests.

**Path B — Worker (agentic inner loop):** When `AgentProfile.AgenticMode: true`, the worker
calls `providerSupportsAgentic` in
[`internal/queue/worker/worker_support.go`](../internal/queue/worker/worker_support.go),
which wraps `gateway.ProviderSupportsChatTools` before entering `processAgentic`. If the
resolved provider returns `SupportsChatTools: false`, the worker silently falls back to the
legacy single-shot JSON path (`runLegacyTask`) and emits a warning log line. No error is
surfaced to the task; execution continues in legacy mode.

As a result, any provider listed as `SupportsChatTools: false` in the Capability Matrix above
will use legacy mode even when `AgenticMode: true` is set on the agent profile. Run
`agentd start -v` to see the fallback warning.

> ⚠️ **Note on Anthropic.** The native `anthropic` adapter returns `SupportsChatTools: true`,
> so `AgenticMode: true` enters `processAgentic` — but its **multi-turn** tool round-trip is
> broken (see §[Provider Deltas — Anthropic](#anthropic)). It can complete at most a single
> tool turn; a second turn cannot carry `tool_result` back. For a working agentic loop on
> Anthropic-family models, route through LiteLLM / Portkey / OpenRouter with `adapter: openai`
> (the **managed path** — see [LLM connector strategy](llm-connector-strategy.md)).

See [`docs/agentic-harness.md`](agentic-harness.md) for the full agentic inner loop
specification, sandbox model, and the behavior table that maps `AgenticMode` × provider to
the resulting execution path.

## Sources

Checked on 2026-05-12:

- Anthropic tool use overview: https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview
- Anthropic Messages API tools field: https://docs.anthropic.com/en/api/messages
- Ollama chat API: https://docs.ollama.com/api/chat
- Ollama tool calling guide: https://docs.ollama.com/capabilities/tool-calling
- llama.cpp function calling notes: https://github.com/ggml-org/llama.cpp/blob/master/docs/function-calling.md
- AI Horde SDK text async request model: https://horde-sdk.readthedocs.io/en/stable/horde_sdk/ai_horde_api/apimodels/generate/text/_async/
