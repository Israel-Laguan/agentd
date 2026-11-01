# T-056: Deferred and On-Demand Tool Loading

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | internal/agent/tools, internal/queue/worker/worker_tools.go |

## Goal

Implement deferred and on-demand tool loading (`load_tools`), inspired by Claudinio Code's lean prompt architecture.

Instead of defining full JSON schemas for every available tool (LSP, MCP, browser, search, tasks) in every request:
1. Provide a minimal core tool set in the initial prompt prefix, with deferred tools listed concisely as one-line summaries in the system prompt.
2. Allow workers to load tool definitions dynamically when needed (or automatically when invoked), appending them to the active session.
3. Keep prompt prefixes small and cache-friendly across multi-turn interactions.

## Done when

- [ ] Core vs deferred tool schema separation implemented in worker tool registry.
- [ ] `load_tools` dynamic schema expansion mechanism functional.
- [ ] Prompt prefix token size significantly reduced when optional/heavy tools are deferred.
- [ ] Tests verify that deferred tools can be loaded and executed cleanly mid-task.

## Notes

- Claudinio reference: Tools and skills arrive when they are needed. Core tools always sent; deferred tools listed in prompt as one-liners and loaded when needed. Cached prefix stays compact and only grows at its end.
