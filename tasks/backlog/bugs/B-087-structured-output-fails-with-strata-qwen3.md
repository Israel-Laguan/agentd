# B-087: Structured output failure with Strata was caused by mock engine (RESOLVED)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done (cause was the mock engine, not Strata or the model) |
| Priority | P2 |
| Sprint | S19-extract-agent-kit |
| Severity | minor |
| Links | internal/gateway/correction/correction.go, internal/gateway/providers/openai.go, Strata inference server |

## Symptoms (Original Report)

When configuring agentd to use a Strata inference server at `http://192.168.1.234:8080/v1`, 
agentd failed with "LLM provider unreachable: status 502" for all internal workflows.

## Root Cause

**The Strata server was running with `--engine mock`**, which responds to every request with 
"Hello from the mock engine." This caused JSON validation to fail when `response_format` was set.

This was **NOT** a Strata limitation or a Qwen3 model limitation.

## Verification with Real Model

After switching to the real model (qwen3.8-flash-next-iq3_s) with valid API key:

### ✅ Direct JSON mode works
```bash
curl http://192.168.1.234:8080/v1/chat/completions \
  -H "Authorization: Bearer <real-key>" \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"What is 2+2?"}],"response_format":{"type":"json_object"}}'

# Returns: {"answer":4} with HTTP 200
```

### ✅ Agentd integration works
With configuration:
```yaml
gateway:
  providers:
    - name: strata
      adapter: openai
      base_url: "http://192.168.1.234:8080/v1"
      api_key: "<real-key>"
      model: "qwen3.8-flash-next-iq3_s"
  order: [strata]
```

Agentd successfully:
- Connects to Strata
- Passes health checks
- Processes chat completions through frontdesk/planner
- Returns structured JSON responses

### ✅ Strata supports response_format
- `json_object` - Verified working
- `json_schema` - Accepted (not fully tested)
- **Constraint**: Do NOT send `tools` + `response_format` together (Strata returns 400). agentd's OpenAI adapter already omits `response_format` when tools are present.

### ⚠️ Model-specific considerations
- Model reasons before answering (returns `reasoning_content`)
- Ensure `max_tokens` is sufficient for reasoning + JSON answer
- Invalid JSON → Strata returns 502 with `structured_output_failed`. agentd treats a 5xx as an unreachable provider (`ErrLLMUnreachable`): the router tries the next provider in `gateway.order` and the failure counts toward the breaker. It is not a JSON correction retry.
- Streaming with JSON format: content arrives only after full validation

## Strata Configuration for agentd

```yaml
gateway:
  order: [strata]
  providers:
    - name: strata
      adapter: openai
      base_url: "http://192.168.1.234:8080/v1"
      api_key: "<your-real-key>"  # Required; mock engine accepted dummy, real model does not
      model: "qwen3.8-flash-next-iq3_s"
      timeout: "30s"
      capabilities: { chat_tools: true }
```

**Important:** Use `strata launch --show-key` to get the real API key.

## Resolution

✅ **RESOLVED** - The issue was a server configuration problem (mock engine enabled), not a 
Strata or model limitation. Strata fully supports `response_format: json_object` and works 
correctly with agentd when using the real model.

## Action Items

- [x] Test direct JSON mode with real model - PASSED
- [x] Test agentd integration with real model - PASSED  
- [x] Update documentation - Strata already documented in docs/openai-compatible-providers.md
- [x] Close bug report - RESOLVED

## Notes

- Tested with Strata v0.6 and Qwen3.8-flash-next-iq3_s
- Server context: 131072 tokens
- Mock engine was the root cause of B-087
