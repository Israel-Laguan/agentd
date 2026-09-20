# US-006: Pipeline debug observability

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | ready |
| Priority | P1 |
| Sprint | S06-stabilize-observe |
| Persona | maintainer |
| Links | [agentic harness](../../../docs/agentic-harness.md) |

## Story

As a **maintainer diagnosing a stalled or slow request**, I want **debug logs spanning chat intake, routing, provider calls, and agentic loop turns**, so that **I can identify exactly where execution stops or becomes delayed**.

## Acceptance criteria

- [ ] Chat intake logs request correlation ID, message count, selected flow, and completion/error outcome without logging secrets or full user content by default.
- [ ] Router logs role, provider/model selection, candidate order, tool capability decisions, cascade attempts, provider errors, and final outcome.
- [ ] Provider adapter logs a sanitized pre-request summary, endpoint/model, timeout, response status, latency, token usage, and decoded tool-call count.
- [ ] Agentic loop logs task ID, session/turn/iteration lifecycle, guard decisions, LLM call start/end, tool dispatch start/end, and terminal result.
- [ ] Logs use structured `slog` fields and preserve existing secret/content redaction behavior.
- [ ] Debug logging is disabled or low-noise by default and can be enabled through the existing log-level configuration.
- [ ] Tests verify key lifecycle events and confirm API keys, authorization headers, and prompt contents are not emitted.
- [ ] A stalled chat request can be traced from HTTP intake through the final gateway/provider operation using one correlation identifier.

## Verification Plan

To verify this story, the following long-horizon test setup is used:

1. **LiteLLM Setup**:
   - Start LiteLLM with a configuration including a primary model (e.g., `poolside/laguna-m.1`) and a fallback model (e.g., `gpt-4o-mini`).
   - Configure the provider API key via its documented env var (e.g., `POOLSIDE_API_KEY` for Poolside, `OPENAI_API_KEY` for OpenAI — see `config.reference.yaml` `gateway.providers[].api_key_env`).
2. **agentd Setup**:
   - Set `LITELLM_API_KEY` to the LiteLLM master key (e.g., `local-key`).
   - Configure `gateway.order: [litellm]` and `adapter: openai`.
   - Enable `agentic_mode: true` for the default agent.
3. **Test Scenario**:
   - Submit a complex chat request (e.g., "Analyze the agentd Go codebase... and find more than 5 repeated types...").
   - Trigger model fallback by simulating an outage or using a known failing primary model.
4. **Observability Check**:
   - Confirm that a single correlation ID traces the request from `POST /v1/chat/completions` $\rightarrow$ Frontdesk $\rightarrow$ Router $\rightarrow$ LiteLLM $\rightarrow$ Provider.
   - Verify that logs reflect the exact timing and outcome of each stage, allowing a maintainer to distinguish between a slow LLM response and a hang in the agentd pipeline.

