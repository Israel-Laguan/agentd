# Milestone 17 — llama.cpp agentic recipe (optional)

**Status**: not started (optional) · **PR scope**: one small docs+config PR · **Depends on**: M16
(the smoke script is the verification tool). **Relates to**: strategy "llama.cpp direct adapter:
keep, frozen; runtime-capability gating".

## Goal

Make "the direct path runs agentic mode" true for **known-good** llama.cpp setups, without
promising tool calling everywhere. Today the llama.cpp entry defaults `SupportsChatTools: false`,
so `AgenticMode: true` silently falls back to legacy JSON mode.

## Background / current state (verified)

- llama.cpp provider targets OpenAI-compatible `/v1/chat/completions` (`internal/gateway/providers/llamacpp.go`),
  `SupportsChatTools` defaults false.
- Tool calling depends on server runtime (`llama-server --jinja`), the chat template, and model
  support — it cannot be asserted statically. `docs/provider-tool-calling.md` already notes this.

## Scope (in)

1. Documented recipe: for a known-compatible setup (e.g. `llama-server --jinja` + a tool-capable
   model), enable agentic mode with an explicit opt-in:
   ```yaml
   gateway:
     providers:
       - name: llamacpp
         adapter: llamacpp
         base_url: "http://127.0.0.1:8080"
         model: "<tool-capable-model>"
         capabilities: { chat_tools: true }
   ```
2. Optional startup capability probe (small, low-risk): instead of relying on the static flag, add
   a one-time probe (a minimal tool-calling request, e.g. at startup or on `warmup_enabled`) that
   sets/refines the effective `SupportsChatTools` for a llamacpp entry and logs the verdict. Keep it
   off by default to avoid surprising extra traffic — gate behind a config option (e.g.
   `options: { probe_tools: true }` on the provider entry).
3. Update the capability matrix row for llama.cpp with the recipe link and the "verified per model"
   caveat.

## Scope (out)

- No changes to how the worker falls back; no forcing tool calling anywhere.
- Not wiring native Anthropic tool round-trip (proxy path covers it — see M13).

## Files to touch (likely)

- `docs/provider-tool-calling.md`, `docs/llm-connector-strategy.md`
- `internal/gateway/providers/llamacpp.go` + test (only for the optional probe)
- `scripts/llm-smoke.sh` usage docs (from M16)

## Verification

- Docs + optional probe test pass.
- Demonstrate (PR description) a smoke-script tool-calling verdict against a live `llama-server`
  with `--jinja` + a tool-capable model; otherwise document "pending hardware/model to verify".
