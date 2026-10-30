# B-087: Structured output (response_format) fails with Strata + Qwen3

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | S19-extract-agent-kit |
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

This indicates the Qwen3 model on Strata does not properly support the OpenAI `response_format` parameter, which agentd requires for all structured outputs (DraftPlan, classification results, etc.).

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

5. Verify direct request to Strata works without `response_format`:
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

Options:

1. **Make JSON mode optional per-provider**: Add a provider config option to disable structured output mode, allowing regular chat completions without `response_format`. This would require changes to the contract system.

2. **Fallback mechanism**: Try the request without `response_format` if it fails with a structured output error, and parse the response as plain text JSON.

3. **Provider capability detection**: Detect at startup whether a provider supports `response_format` and disable it for providers that don't. This requires a probe request.

4. **Documentation**: Document that Strata (and similar providers) require models that support `response_format` for full agentd functionality.

Option 1 is the most flexible but requires the most changes. Option 2 might work but could lead to inconsistent behavior. Option 3 is clean but adds startup latency. Option 4 is the minimal change but doesn't solve the problem.

## Workaround

Use a different OpenAI-compatible provider that properly supports `response_format: {"type": "json_object"}`, such as:
- LiteLLM
- vLLM
- Ollama (with supported models)
- Direct OpenAI API

Or use the Strata endpoint directly without going through agentd's planning system.

## Notes

- Tested with Strata 0.6 (BaseHTTP/0.6 Python/3.13.5) and Qwen3.8-flash-next
- The Strata server does support the `/v1/models` endpoint, which is used for health checks
- The issue is specific to the `response_format` parameter, not general connectivity
- Other models on Strata may work if they support structured output
