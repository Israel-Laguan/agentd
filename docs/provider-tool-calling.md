# Provider Tool Calling

agentd keeps provider tool support conservative. A provider must keep
`SupportsChatTools` false until it has request mapping, response parsing, and
fixture tests for that provider's actual wire format.

## Capability Matrix

| Provider | `SupportsChatTools` | Status | Notes |
| --- | --- | --- | --- |
| OpenAI | `true` | Verified | Sends OpenAI-compatible `tools` and parses `tool_calls` in provider fixture tests. |
| Anthropic | `false` | Not wired | Native Messages API tools use top-level `tools`, `input_schema`, `tool_use`, and `tool_result` content blocks. |
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

Keep `SupportsChatTools` false until request mapping, tool-use parsing, and a
tool-result round trip are covered by fixtures.

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

## Sources

Checked on 2026-05-12:

- Anthropic tool use overview: https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview
- Anthropic Messages API tools field: https://docs.anthropic.com/en/api/messages
- Ollama chat API: https://docs.ollama.com/api/chat
- Ollama tool calling guide: https://docs.ollama.com/capabilities/tool-calling
- llama.cpp function calling notes: https://github.com/ggml-org/llama.cpp/blob/master/docs/function-calling.md
- AI Horde SDK text async request model: https://horde-sdk.readthedocs.io/en/stable/horde_sdk/ai_horde_api/apimodels/generate/text/_async/
