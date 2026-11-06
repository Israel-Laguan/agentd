# Harness backlog: planned in agentd, never executed

Bugs and tasks that agentd planned for its builtin agent harness and never ran. agentd moved the harness out on 2026-11-05 (owner decision: external agents only; agentd's `docs/harness-extraction.md`). These files go with the harness to its own repository, which can add them to its backlog. agentd tracks none of them.

IDs are agentd's, kept so each item can be traced to its history there. "(rest)" marks the part of an agentd ticket that was never done.

| ID | Title | Priority |
| --- | --- | --- |
| [B-089 (rest)](B-089-rest-budget-cut-and-edit-tool.md) | Agentic budget, the cut of the latest tool result, an edit tool | P2 |
| [T-064](T-064-deferred-tool-loading.md) | Deferred and on-demand tool loading | P2 |
| [B-057](B-057-delegate-tool-does-not-list-subagent-names.md) | `delegate` tool schema does not list the subagent names | P3 |
| [B-063 (rest)](B-063-rest-plugin-mounting.md) | Mount plugins safely, with real capability adapters | P3 |
| [B-061 (rest)](B-061-rest-capability-routing.md) | Capability routing routes nothing | P3 |

## Config keys that were never implemented

agentd documented these as "not implemented" for the builtin loop; nothing read them:

- `agentic.tool_rate_limits` (no hook enforced them);
- `agentic.review` (the review loop never ran);
- `agentic.skills` (skills were read from `queue.skills` instead).

## Known weaknesses, measured in agentd

From agentd's `docs/acp-agents.md` (S17 SP-022 and the S19 live smoke, small samples):

- On an ambiguous task the loop reported COMPLETED with nothing done in 2 of 3 runs (SP-022).
- It once wrote a failing test and reported success when the sandbox had no Go toolchain (SP-022; the toolchain bind came later, agentd B-081).
- In the S19 smoke it asked a human mid-change and left the workspace broken (`CountWords` lost, a missing import).
