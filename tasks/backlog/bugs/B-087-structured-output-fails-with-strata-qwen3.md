# B-087: Structured output fails with Strata + Qwen3 (output budget or response_format)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | S19-extract-agent-kit |
| Severity | major |
| Links | internal/gateway/correction/correction.go (GenerateJSONWithUsage sets JSONMode=true), internal/gateway/providers/openai.go, Strata inference server |

## Symptoms

When configuring agentd to use a Strata inference server running Qwen3.8-flash-next at `http://192.168.1.234:8080/v1`, agentd fails to use the endpoint for its internal workflows (intake, planning, etc.).

The connection succeeds and the endpoint passes health checks (when an API key is configured), but any request that requires structured JSON output returns:
```
LLM provider unreachable: status 502
```

Direct HTTP requests to the Strata endpoint work fine for regular chat completions.

## Cause

Agentd's `correction.GenerateJSONWithUsage` function sets `req.JSONMode = true` for all contract-based calls, which the OpenAI provider translates to `response_format: {"type": "json_object"}` in the request body.

The Strata server with Qwen3.8-flash-next returns HTTP 502 Bad Gateway with:
```json
{"error": {"type": "structured_output_failed", "code": "structured_output_failed", "message": "structured output was incomplete (finish_reason=length); increase the output budget"}}
```

**Hypothesis, not yet confirmed:** the error itself says the output budget ran out (`finish_reason=length`), not that `response_format` is unsupported. Two facts point the same way:

- agentd sends no `max_tokens` on the intake calls. Their stage requests leave `MaxTokens` at 0, and the OpenAI adapter omits the field (`omitempty`), so Strata's server-side default budget applies.
- Qwen3 models can spend output tokens on reasoning before the JSON, which a small default budget would cut off.

Verify the budget first: repeat the structured request with `response_format` and a large `max_tokens`. Only if that still fails is `response_format` itself unsupported.

## Reproduction

1. Configure agentd with Strata endpoint:
   ```yaml
   gateway:
     order: [strata]
     providers:
       - name: strata
         adapter: openai
         base_url: "http://192.168.1.234:8080/v1"
         api_key: "dummy-key"
         model: "qwen3.8-flash-next"
   ```

2. Start agentd with `--skip-llm-warmup`

3. Send a chat completion request to `POST /v1/chat/completions`

4. Observe 502 error in agentd logs:
   ```
   openai: request failed, status=502, error="LLM provider unreachable: status 502"
   ```

5. Verify a direct request to Strata works without `response_format` (note this changes two things at once: no `response_format` and an explicit `max_tokens`, so it does not isolate the cause; add a run with `"response_format": {"type": "json_object"}` and a large `max_tokens`):
   ```bash
   curl -X POST http://192.168.1.234:8080/v1/chat/completions \
     -H "Content-Type: application/json" \
     -d '{"model": "qwen3.8-flash-next", "messages": [{"role": "user", "content": "Hello"}], "max_tokens": 50}'
   # Returns 200 OK
   ```

## Impact

- Agentd cannot use Strata inference servers with Qwen3 (and potentially other models) for its normal operations
- Limits provider compatibility for users who want to use Strata as their local OpenAI-compatible provider
- May affect other OpenAI-compatible providers that don't fully support `response_format`

## Suggested fix

First confirm the cause (see Cause). If it is the output budget, the fix is to send a `max_tokens` large enough for structured calls (per stage, or a provider default), not to drop `response_format`.

If `response_format` is really unsupported, options:

1. **Make JSON mode optional per-provider**: Add a provider config option to disable structured output mode, allowing regular chat completions without `response_format`. This would require changes to the contract system.

2. **Fallback mechanism**: Try the request without `response_format` if it fails with a structured output error, and parse the response as plain text JSON.

3. **Provider capability detection**: Detect at startup whether a provider supports `response_format` and disable it for providers that don't. This requires a probe request.

4. **Documentation**: Document that Strata (and similar providers) require models that support `response_format` for full agentd functionality.

Option 1 is the most flexible but requires the most changes. Option 2 might work but could lead to inconsistent behavior. Option 3 is clean but adds startup latency. Option 4 is the minimal change but doesn't solve the problem.

## Workaround

Not yet verified: raise Strata's default output budget, if it has one. Otherwise use a different OpenAI-compatible provider that supports `response_format: {"type": "json_object"}`, such as:
- LiteLLM
- vLLM
- Ollama (with supported models)
- Direct OpenAI API

Or use the Strata endpoint directly without going through agentd's planning system.

## Notes

- Tested with Strata 0.6 (BaseHTTP/0.6 Python/3.13.5) and Qwen3.8-flash-next
- agentd's health check for an `adapter: openai` provider with an API key only checks that a key is configured (`tryAPIKeyHealth` in `internal/config/health.go`). It does not probe `/v1/models`, so passing it says nothing about connectivity. Only `health: llamacpp` probes `/v1/models`.
- The issue is specific to structured-output requests, not general connectivity
- Other models on Strata may work if they support structured output
