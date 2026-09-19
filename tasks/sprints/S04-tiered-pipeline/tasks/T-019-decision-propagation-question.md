# T-019: Decision artifact propagation — open design question

| Field | Value |
| --- | --- |
| Type | question |
| Status | backlog |
| Priority | P2 |
| Sprint | S04-tiered-pipeline |
| Estimate | — |
| Links | [tiered-execution](../../../../docs/tiered-execution.md), T-016 |

## Context

CodeRabbit and cubic-dev-ai both flagged (independently) that the M3 worker
modes never propagate the Decision step's output (`touch_list`, `checks`) to
execute/verify, and that `SplitIntoTieredDAG`'s children lose their
`DependsOn` predecessor once persisted (the `models.Task.DependsOn` field has
no DB column and is never restored on load). A same-day fix pass
(2026-09-19) resolved the adjacent panic/no-op bugs these findings pointed
at (`task.DependsOn[0]` indexing, `injectContextPack` losing its mutation,
the recursive re-tiering gate) by leaning on the existing `ListParentTasks`
(SPAWNED_BY) fallback, which already resolves correctly today because the
DAG's origin task is reliably the oldest row in `task_relations`.

That fallback does **not** give a reliable handle on the *immediate
predecessor step* (e.g. decision's own task, from execute's point of view) —
`ListParentTasks` returns all relation types together, ordered by
`created_at`, and the four tiered children share the same timestamp. So
propagating and enforcing the Decision artifact (touch_list/checks) into
execute and verify — which both reviewers rate P1 — needs a real answer to:

## Open question

**How should a tiered step address its specific predecessor step (not just
the DAG's origin task)?** Options seen so far:

1. Add a dedicated, typed predecessor pointer (e.g. `PredecessorTaskID` on
   `models.Task`, persisted as a real column) instead of overloading
   `DependsOn`/relations lookup.
2. Keep using `task_relations`, but query `DEPENDS_ON` specifically (typed,
   not the generic `ListParentTasks`) and rely on the one-edge invariant
   `SplitIntoTieredDAG` already produces.
3. Have each step's committed artifact self-describe its lineage (task ID +
   version) so downstream steps resolve it by content rather than by graph
   traversal — closer to how `ContextPack`/`PackFilePath(version)` already
   work.

Whichever shape is chosen also answers T-017's NEEDS_CONTEXT pack
re-versioning/invalidation requirement, so it's worth deciding once rather
than per-ticket.

## Done when

- [ ] Human decision recorded on which predecessor-resolution mechanism to use
- [ ] Follow-up implementation ticket filed (Decision persistence + injection
      into execute/verify, enforcement of touch_list/checks) referencing the
      chosen mechanism
- [ ] `internal/kanban/tiered_dag.go`'s missing DAG-shape validation (first
      child READY + empty DependsOnID, chain integrity) folded into the same
      follow-up rather than bolted on separately
