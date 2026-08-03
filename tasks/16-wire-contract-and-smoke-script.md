# Milestone 16 — Wire-contract suite + conformance smoke script

**Status**: not started · **PR scope**: one code+tooling PR · **Depends on**: nothing hard
(positioning from M13 helps docs). **Relates to**: strategy "OpenAI-compatible adapter = the ONE
wire path".

## Goal

Prove the single wire contract (OpenAI Chat Completions) that **both** topologies depend on, and
provide a script that certifies any endpoint (llama.cpp direct, LiteLLM managed, OpenAI, vLLM)
satisfies the contract. This operationalizes "the direct path should work — and here's the verdict
for your model."

## Background / current state (verified)

- The openai adapter (`internal/gateway/providers/openai.go`) already handles Chat Completions,
  tools, `tool_calls`, embeddings. Much fixture coverage exists in `openai_test.go`,
  `openai_timeout_test.go`, `gateway_feature_tool_test.go`, but it is not organized/described as
  *the* contract both topologies rely on.
- No standalone conformance probe exists today.

## Scope (in)

1. **Wire-contract test suite**: consolidate/organize openai-adapter tests around an explicit
   contract matrix:
   - tools request shape (`tools` serialization, `type: function`, parameters)
   - `tool_calls` response parsing
   - `tool_call_id` round-trip on `role: tool` messages
   - error / HTTP-status mapping
   - timeout behavior (`ctx` deadline)
   - JSON-mode interaction when tools are present
   - embeddings passthrough
   Mark the suite as the contract both the direct and managed paths must satisfy. Prefer a shared
   fixture HTTP server (`httptest`) as the reference endpoint.
2. **`scripts/llm-smoke.sh`** (POSIX sh, `curl` or `wget`): given `base_url`, optional api key,
   and `model`, run three probes and print a per-model verdict:
   - text generation (`/v1/chat/completions` with a plain user turn)
   - JSON mode (a `response_format`/JSON prompt and a parse check)
   - tool calling (send a `tools` definition and assert the response parses `tool_calls` or reports
     "no tool_calls returned / not supported")
   Exit non-zero only on hard connectivity failure; print capabilities as a verdict table.
3. Docs: a section (in `docs/llm-connector-strategy.md` or `docs/api-testing.md`) covering how to
   run the smoke script against llama.cpp (direct) and LiteLLM (managed), and how to interpret the
   verdict.

## Scope (out)

- No streaming support (declared non-goal).
- No changes to provider behavior — only tests/scripts/docs.
- Not flipping `SupportsChatTools` flags (that is M17).

## Files to touch (likely; final set via search)

- `internal/gateway/providers/openai_test.go` (+ possibly a new `wire_contract_test.go`)
- `scripts/llm-smoke.sh` (new)
- `internal/gateway/providers/http.go` (only if a helper is needed/reused)
- docs additions

## Verification

- `go test ./internal/gateway/...` green.
- `chmod +x scripts/llm-smoke.sh`; demonstrate against the fixture server (or a documented mock);
  optionally capture a run against a live llama.cpp / LiteLLM as evidence in the PR description.
