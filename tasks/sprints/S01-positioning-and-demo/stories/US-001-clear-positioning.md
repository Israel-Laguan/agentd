# US-001: Clear public positioning

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | in-progress |
| Priority | P0 |
| Sprint | S01-positioning-and-demo |
| Persona | adopter |
| Links | [product-plan Phase 0](../../../docs/product-plan.md) |

## Story

As an **adopter scanning GitHub**, I want **a one-line description and contrast vs coding CLIs**, so that **I don’t file agentd under “yet another coding agent.”**

## Acceptance criteria

- [x] GitHub About matches the product-plan one-liner — `Local daemon that turns an approved plan into a durable Kanban board ... SQLite source of truth.` (T-001, apply via gh)
- [x] Topics include at least: `agent-harness`, `local-first`, `kanban`, `sqlite`, `multi-agent`, `golang` (T-001)
- [x] README hero states what it is / is not / why local — `README.md:1`
- [x] `docs/why-agentd.md` (or README section) contrasts Claude Code, OpenHands, agent-kanban, HAR — `docs/why-agentd.md:1`

## Notes

- Children: T-001, T-002, T-003
- * `in-progress` — first two ACs depend on `T-001` `gh repo edit` About/topics apply. Instruction is prepared; `gh repo view --json description,repositoryTopics` traceability is the outstanding repo-admin action carried per `retro/RETRO.md` (due 2026-09-15) and `tasks/T-001-github-about-topics.md:21`. Close is conditional on that PR evidence.
