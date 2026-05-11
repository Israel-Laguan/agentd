# Agentic harness — implementation roadmap

Phased work to add an **opt-in inner agentic loop** (tool calling with accumulated messages inside `Worker.Process`) while keeping the **default** single-shot JSON worker unchanged until explicitly enabled.

Canonical architecture and terminology: [docs/agentic-harness.md](agentic-harness.md).

## Phase dependency overview

```mermaid
flowchart LR
  p1[Phase1_Gateway_tools]
  p2[Phase2_Parse_tool_calls]
  p3[Phase3_Prompt_protocol]
  p4[Phase4_Tool_registry]
  p5[Phase5_Safety_budgets]
  p6[Phase6_Mode_and_loop]
  p1 --> p2 --> p3 --> p4 --> p5 --> p6
```

Post-MVP items (tasks 08–12) extend observability, routing, truncation, tests, and additional providers; see the end of this document.

---

## Phase 1: Gateway tool definitions

**Goal**: `AIRequest` can carry OpenAI-style tool schemas; OpenAI provider sends `tools` and avoids conflicting JSON response formats when tools are present.

**Depends on**: nothing.

**Primary files**: [`internal/gateway/spec/spec.go`](../internal/gateway/spec/spec.go), [`internal/gateway/providers/openai.go`](../internal/gateway/providers/openai.go), [`internal/gateway/exports.go`](../internal/gateway/exports.go) if types are re-exported.

**Verification**: Existing tests pass; new test marshals an `AIRequest` with tools and asserts JSON body shape (and correct interaction with JSON mode when tools are set).

**Task**: [tasks/01-tool-definitions-in-gateway-spec.md](../tasks/01-tool-definitions-in-gateway-spec.md)

---

## Phase 2: Tool call parsing in responses

**Goal**: `AIResponse` exposes parsed `tool_calls` from the provider so callers can branch without ad-hoc JSON.

**Depends on**: Phase 1 (types and request wiring should exist; parsing can land immediately after).

**Primary files**: [`internal/gateway/spec/spec.go`](../internal/gateway/spec/spec.go), [`internal/gateway/providers/openai.go`](../internal/gateway/providers/openai.go).

**Verification**: Unit test with stubbed OpenAI JSON containing `tool_calls` asserts populated `AIResponse.ToolCalls`.

**Task**: [tasks/02-tool-call-parsing-in-responses.md](../tasks/02-tool-call-parsing-in-responses.md)

---

## Phase 3: Prompt message tool protocol

**Goal**: `PromptMessage` and marshaling support assistant messages with `tool_calls` and tool messages with `tool_call_id` (OpenAI chat format). No worker behavior change required yet.

**Depends on**: Phases 1–2.

**Primary files**: [`internal/gateway/spec/spec.go`](../internal/gateway/spec/spec.go), [`internal/gateway/providers/openai.go`](../internal/gateway/providers/openai.go) (or a dedicated mapper if introduced).

**Verification**: Round-trip / marshal tests for multi-turn assistant+tool message lists.

**Task**: [tasks/03-prompt-message-tool-protocol.md](../tasks/03-prompt-message-tool-protocol.md)

---

## Phase 4: Tool executor registry

**Goal**: Register `bash`, `read`, and `write` tools with JSON schemas; execute them via sandbox and filesystem rules **without** an LLM loop.

**Depends on**: Phase 3 (tool result messages should match the wire protocol).

**Primary files**: new under [`internal/queue/worker/`](../internal/queue/worker/) or [`internal/sandbox/`](../internal/sandbox/) per task; integrate with existing `BashExecutor` and path safety.

**Verification**: Unit tests for each tool’s argument validation and execution (mock sandbox where appropriate).

**Task**: [tasks/04-tool-executor-registry.md](../tasks/04-tool-executor-registry.md)

---

## Phase 5: Iteration budget and safety

**Goal**: Max tool iterations, interaction with task wall-clock deadline and token budget, clear behavior when limits are exceeded (forced completion path or handoff). Tool failures return as tool output, not outer retry.

**Depends on**: Phase 4.

**Primary files**: [`internal/queue/worker/worker.go`](../internal/queue/worker/worker.go), [`internal/config/`](../internal/config/) as needed for limits.

**Verification**: Unit tests for cap exhaustion and timeout interaction; no new bus event types in this phase.

**Task**: [tasks/05-iteration-budget-and-safety.md](../tasks/05-iteration-budget-and-safety.md)

---

## Phase 6: Opt-in mode and inner loop orchestration

**Goal**: (6a) Profile or config selects **agentic** vs **legacy** worker path (default legacy). (6b) In agentic mode, `Worker.Process` runs the LLM → tool → result loop using accumulated messages, registry, and guards from prior phases.

**Depends on**: Phases 3–5.

**Primary files**: [`internal/queue/worker/worker.go`](../internal/queue/worker/worker.go), [`internal/models/`](../internal/models/) if profile fields are added, config as needed.

**Verification**: Manual or automated scenario with agentic mode on; regression that default profile still uses `GenerateJSON` / single-shot path.

**Tasks**:

- [tasks/06-agentic-worker-mode-toggle.md](../tasks/06-agentic-worker-mode-toggle.md)
- [tasks/07-worker-inner-loop-orchestration.md](../tasks/07-worker-inner-loop-orchestration.md)

---

## Post-MVP (ordered by task id)

| Task | Topic |
| --- | --- |
| [tasks/08-tool-call-events-and-sse-observability.md](../tasks/08-tool-call-events-and-sse-observability.md) | New event types and scrubbed payloads for cockpit / SSE. |
| [tasks/09-provider-capabilities-and-fallback.md](../tasks/09-provider-capabilities-and-fallback.md) | Capability flags; unsupported providers stay on legacy path; OpenAI first. |
| [tasks/10-context-truncation-for-tool-history.md](../tasks/10-context-truncation-for-tool-history.md) | Truncate accumulated inner history using existing strategies. |
| [tasks/11-agentic-loop-integration-tests.md](../tasks/11-agentic-loop-integration-tests.md) | End-to-end worker tests with mock gateway. |
| [tasks/12-provider-expansion-followups.md](../tasks/12-provider-expansion-followups.md) | Anthropic, Ollama-compatible, and other provider-specific tool formats. |

---

## Risk register

| Risk | Mitigation |
| --- | --- |
| Breaking default worker | Agentic mode off by default; legacy `workerResponse` path unchanged. |
| Infinite tool chatter | Phase 5 caps, deadlines, budgets. |
| Provider lacks tools | Phase 9 routing; until then, only enable agentic mode for OpenAI. |
| Token explosion | Existing `BudgetTracker` across inner iterations; truncation task 10. |
