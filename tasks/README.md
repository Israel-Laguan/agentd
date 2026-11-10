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
  backlog/                  ← not yet scheduled (tracked in git)
    stories/ tasks/ bugs/ spikes/
  sprints/                  ← local-only working state (gitignored, see below)
    S01-…/                  ← one folder per sprint
      README.md             ← sprint goal + board table
      stories/ tasks/ bugs/ spikes/
      retro/RETRO.md
```

`tasks/sprints/` is untracked (`.gitignore`) — it's local sprint-in-progress
state, not shared history. Anything worth keeping past a sprint's life
belongs in `backlog/` (unfinished work) or a retro action folded back into
this README/backlog before the sprint folder is cleared.

## Workflow

1. Capture new ideas in `backlog/` with the right type + next free ID.
2. At planning, move (or copy) items into the sprint folder and set `Sprint` + `Status: ready`.
3. Work in priority order; keep the sprint `README.md` table in sync.
4. Close the sprint with `retro/RETRO.md`; move unfinished items back to backlog or the next sprint. Reconcile every item against `gh pr list --state merged --limit 1000` first, not against the board.
5. IDs never reuse. Status vocabulary stays fixed (see templates).
6. When a PR merges, set every ticket's Status and the sprint board in the same sitting. (S18, S19 and S20 each closed with stale ticket statuses.)
7. The Status cell holds one word from the list below. Put merge details (PR, sha, date) in the Sprint cell or Notes, not in Status.
8. Before merge, address each review thread with a commit, or with a reply that says why not, then mark the conversation resolved. A thread closed with neither a commit nor a reply is a finding the next sprint inherits (S20 left 45 open on merged PRs).

## ID allocation

| Series | Next free (seeded) |
| --- | --- |
| US- | US-012 |
| T- | T-079 |
| B- | B-098 |
| SP- | SP-030 |

Update this table when you mint IDs.

## Status values

`backlog` → `ready` → `in-progress` → `review` → `done` (or `dropped`)

## Linking to product plan

Active sprint lives under the local (gitignored) `sprints/` folder — not linked here since it isn't committed. Sprint goals should cite phases in [`docs/product-plan.md`](../docs/product-plan.md). Spikes for tiered execution cite [`docs/tiered-execution.md`](../docs/tiered-execution.md).
