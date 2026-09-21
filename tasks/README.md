# tasks/ — sprint planning for agentd

This folder is the **human** backlog and sprint board (markdown). It is separate from:

- the **runtime** Kanban board inside agentd (`docs/api-tasks.md`)
- feature implementation checklists under `.kiro/specs/…/tasks.md`
- strategy docs: [`docs/product-plan.md`](../docs/product-plan.md), [`docs/tiered-execution.md`](../docs/tiered-execution.md)

## Do we have clear task definitions?

**Before this folder:** no. Plans and roadmaps existed; work items were not typed or sprint-scoped.

**Now:** every work item is one of:

| Type | Prefix | Use when |
| --- | --- | --- |
| **User story** | `US-` | Outcome for a persona (operator, contributor, adopter) |
| **Task** | `T-` | Concrete delivery work that closes acceptance criteria |
| **Bug** | `B-` | Broken / incorrect behavior |
| **Spike** | `SP-` | Time-boxed learning; ends in a doc/go-no-go, not production code by default |
| **Retro** | `RETRO` | End-of-sprint reflection + actions |

Copy from [`templates/`](templates/) — do not edit templates in place for real work.

## Layout

```text
tasks/
  README.md                 ← you are here
  templates/                ← blank forms
  backlog/                  ← not yet scheduled
    stories/ tasks/ bugs/ spikes/
  sprints/
    S01-…/                  ← one folder per sprint
      README.md             ← sprint goal + board table
      stories/ tasks/ bugs/ spikes/
      retro/RETRO.md
```

## Workflow

1. Capture new ideas in `backlog/` with the right type + next free ID.
2. At planning, move (or copy) items into the sprint folder and set `Sprint` + `Status: ready`.
3. Work in priority order; keep the sprint `README.md` table in sync.
4. Close the sprint with `retro/RETRO.md`; move unfinished items back to backlog or the next sprint.
5. IDs never reuse. Status vocabulary stays fixed (see templates).

## ID allocation

| Series | Next free (seeded) |
| --- | --- |
| US- | US-007 |
| T- | T-023 |
| B- | B-003 |
| SP- | SP-007 |

Update this table when you mint IDs.

## Status values

`backlog` → `ready` → `in-progress` → `review` → `done` (or `dropped`)

## Linking to product plan

Active sprint: [`sprints/S06-stabilize-observe/`](sprints/S06-stabilize-observe/) (PR discipline: [`sprints/S03-tiered-foundation/PR-PLAN.md`](sprints/S03-tiered-foundation/PR-PLAN.md)). Sprint goals should cite phases in [`docs/product-plan.md`](../docs/product-plan.md). Spikes for tiered execution cite [`docs/tiered-execution.md`](../docs/tiered-execution.md).
