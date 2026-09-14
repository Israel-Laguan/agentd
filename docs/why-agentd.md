# Why agentd

Short contrast for people comparing harnesses. Full plan: [product-plan.md](product-plan.md).

**One line:** local daemon that turns an **approved plan** into a durable **Kanban board** and runs **sandboxed workers** against it — model-agnostic, SQLite source of truth.

## vs the usual suspects

| | agentd | Claude Code / Codex / Cursor | OpenHands / Aider / Cline | agent-kanban | HAR |
| --- | --- | --- | --- | --- | --- |
| **Owns** | Board + workers + memory | Chat/coding loop + tools | Full think–act loop in a repo | Kanban UI that *drives* another agent (MCP) | Isolation/verify around *existing* agents |
| **Source of truth** | SQLite task lifecycle / DAG | Session transcript + files | Session + repo | Board for orchestration; execution elsewhere | Worktrees + verify; loop elsewhere |
| **Autonomy gate** | Plan → **human approve** → materialize | Often acts in-repo, asks later | Loop-first | Human manages board; agent is external | Wraps whatever agent you plug in |
| **Failure mode** | Board states / `HUMAN` handoffs | Stuck or noisy chat | Stuck or retry in-session | Depends on wrapped agent | Depends on wrapped agent |
| **Best at** | Multi-step projects, cheap/local models, unattended recovery | Fast single-session coding UX | Benchmark / open coding loops | Local Kanban *on top of* Claude/Cline | Safe sandboxes around strong CLIs |

## What that means

- If you want the **best coding agent in the editor**, use Claude Code / Cursor / OpenHands — agentd is not trying to win that race.
- If you want **Kanban that drives Claude Code**, agent-kanban / HAR are closer cousins; agentd instead **owns the workers** as well as the board.
- If you want a **self-hosted work runtime** (approved work order → durable tasks → sandboxed execution → human tickets on failure), that is agentd’s slot.

## Related

- Architecture / invariants: [architecture.md](architecture.md)
- Demo path (when present): [demo.md](demo.md)
- Tiered execution (design): [tiered-execution.md](tiered-execution.md)
