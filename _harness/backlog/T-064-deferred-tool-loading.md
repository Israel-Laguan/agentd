# T-064: Deferred and On-Demand Tool Loading

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog (planned in agentd, never executed; documented in the harness-extraction plan on 2026-11-05  — the builtin loop still runs in agentd, its removal is a pending T-071/T-072 step) |
| Priority | P2 |
| Sprint | — (agentd S20, not run) |
| Parent | none |
| Estimate | M |
| Links | internal/agent/tools, internal/queue/worker/worker_tools.go |

## Goal

Implement deferred and on-demand tool loading (`load_tools`), inspired by Claude Code's lean prompt architecture.

Instead of defining full JSON schemas for every available tool (LSP, MCP, browser, search, tasks) in every request:

1. Provide a minimal core tool set in the initial prompt prefix, with deferred tools listed concisely as one-line summaries in the system prompt.
2. Allow workers to load tool definitions dynamically when needed (or automatically when invoked), either registering loaded schemas in the next request's `tools` parameter (the gateway only yields dispatchable `tool_calls` for registered tools) or defining a generic invocation protocol for message-content schemas.
3. Keep prompt prefixes small and cache-friendly across multi-turn interactions.

## Done when

- [ ] Core vs deferred tool schema separation implemented in worker tool registry.
- [ ] `load_tools` dynamic schema expansion mechanism functional.
- [ ] Prompt prefix token size significantly reduced when optional/heavy tools are deferred.
- [ ] Tests verify that deferred tools can be loaded and executed cleanly mid-task.

## Notes

- Claude Code reference: Tools and skills arrive when they are needed. Core tools always sent; deferred tools listed in prompt as one-liners and loaded when needed. Cached prefix stays compact and only grows at its end.
- Conditional (2026-11-02): only the builtin loop has a tool registry to defer; external ACP agents load their own tools. Build it only if S19 keeps the builtin loop (SP-024); otherwise drop it at sprint start.
