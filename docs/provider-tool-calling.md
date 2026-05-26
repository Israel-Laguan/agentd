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
| OpenAI | `true` | Verified | Sends OpenAI-compatible `tools` and parses `tool_calls` in provider fixture tests. |
| Anthropic | `true` | Verified | Maps `AIRequest.Tools` to `tools` with `name`, `description`, `input_schema`; parses `tool_use` content blocks; fixture tests cover request/response. |
| Gemini | `true` | Verified | OpenAI-compatible endpoint via `name: gemini`, `adapter: openai` (legacy `adapter: gemini` alias is accepted). Sends `tools` and parses `tool_calls` like OpenAI. |
| Ollama | `false` | Not wired | `/api/chat` supports a `tools` field and returns `message.tool_calls`, but support depends on server and model behavior. |
| llama.cpp | `false` | Not wired | OpenAI-style function calling depends on runtime setup such as `llama-server --jinja`, chat templates, and model support. |
| AI Horde | `false` | Unsupported | The current provider uses async text generation with prompt and Kobold-style generation parameters, not a chat tool-call contract. |

## Provider Deltas

### Anthropic

Convert `AIRequest.Tools` from OpenAI function objects to Messages API tools:
`name`, `description`, and `input_schema`. Tool results need Anthropic
`tool_result` content blocks rather than OpenAI `role: tool` messages.

Parse response `content` blocks with `type: "tool_use"` into
`AIResponse.ToolCalls`. The block `input` object should be serialized into
`ToolCallFunction.Arguments`; text blocks remain normal response content.

Implemented: request mapping, tool-use parsing, and fixture tests verified.

### Ollama

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
