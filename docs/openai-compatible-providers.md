# OpenAI-Compatible Cloud Providers

agentd can route through any vendor that exposes an OpenAI-compatible chat completions API.
Use the `gateway.providers` list with `adapter: openai` to add cloud vendors alongside — or
instead of — the built-in `openai` slot.

For local OpenAI-compatible servers (llama.cpp, LM Studio, vLLM), see
[`docs/llamacpp-quickstart.md`](llamacpp-quickstart.md).

## Configuration Pattern

**Recommended:** add a `gateway.providers` entry for each vendor (distinct `name`, shared
`adapter: openai`). This gives correct `ProviderUsed` identity in logs and API responses
and supports cascade fallback between vendors.

**Legacy workaround:** repoint the built-in `openai` slot at any OpenAI-compatible endpoint
by setting `gateway.openai.base_url` and duplicating the vendor key into `gateway.openai.api_key`
(or `OPENAI_API_KEY`). Responses still report `ProviderUsed: "openai"` because the slot name
does not change — prefer a named `providers` entry when identity matters (see
[`docs/reference.md`](reference.md)).

Keys used by every `providers` entry:

| Key | Description |
| --- | --- |
| `name` | Unique identifier used in `gateway.order` and `ProviderUsed` response fields |
| `adapter` | `openai` for any OpenAI-compatible endpoint |
| `base_url` | Vendor's base URL (up to but not including `/chat/completions`) |
| `api_key_env` | Environment variable holding the API key (preferred over inline `api_key`) |
| `model` | Model name as the vendor expects it |
| `capabilities.chat_tools` | Override tool-calling support; the `openai` adapter defaults to `true` |

List the vendor name(s) in `gateway.order`. agentd tries each provider in order and advances
to the next on error, so cascade fallback works automatically.

## Named Examples

### Groq

```yaml
gateway:
  providers:
    - name: groq
      adapter: openai
      base_url: "https://api.groq.com/openai/v1"
      api_key_env: GROQ_API_KEY
      model: "llama-3.3-70b-versatile"
  order: [groq]
```

Add `GROQ_API_KEY=<key>` to `.env` or export it in the environment.

### Together AI

```yaml
gateway:
  providers:
    - name: together
      adapter: openai
      base_url: "https://api.together.xyz/v1"
      api_key_env: TOGETHER_API_KEY
      model: "meta-llama/Llama-3-70b-chat-hf"
  order: [together]
```

Add `TOGETHER_API_KEY=<key>` to `.env`.

### Poolside

```yaml
gateway:
  providers:
    - name: poolside
      adapter: openai
      base_url: "https://inference.poolside.ai/v1"
      api_key_env: POOLSIDE_API_KEY
      model: "poolside/laguna-m.1"
  order: [poolside]
```

Add `POOLSIDE_API_KEY=<key>` to `.env`.

## Cascade Fallback

List multiple providers in `order` for automatic fallback:

```yaml
gateway:
  providers:
    - name: groq
      adapter: openai
      base_url: "https://api.groq.com/openai/v1"
      api_key_env: GROQ_API_KEY
      model: "llama-3.3-70b-versatile"
    - name: together
      adapter: openai
      base_url: "https://api.together.xyz/v1"
      api_key_env: TOGETHER_API_KEY
      model: "meta-llama/Llama-3-70b-chat-hf"
  order: [groq, together]
```

## Tool Calling

The `openai` adapter defaults to `SupportsChatTools: true`, so all examples above support
agentic inner-loop tool calling out of the box. If a vendor does not support tool definitions,
add `capabilities: {chat_tools: false}` to its entry.

## ProviderUsed Identity

`ProviderUsed` in API responses and startup logs reflects the `name` field from the provider
entry (e.g. `"groq"`, `"poolside"`), not the adapter type. This is different from the
built-in `openai` slot, where `ProviderUsed: "openai"` is reported regardless of what
`gateway.openai.base_url` points to. See [`docs/reference.md`](reference.md) for more detail.
