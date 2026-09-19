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
DAG's origin task is created (and therefore ordered first by
`tasks.created_at`) before the split — `ListParentTasks` orders by
`tasks.created_at`, and every `task_relations` row for the DAG is inserted
in one transaction in `PersistTieredDAG`, so the children share the same
relation timestamp and the origin's earlier task row is what wins.

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

- [x] Human decision recorded on which predecessor-resolution mechanism to use — **Option 2: typed DEPENDS_ON query** (no schema changes, aligns with existing DAG structure)
- [x] Follow-up implementation ticket filed (Decision persistence + injection
      into execute/verify, enforcement of touch_list/checks) — folded into T-017/T-018 implementation
- [x] `internal/kanban/tiered_dag.go`'s missing DAG-shape validation (first
      child READY + empty DependsOnID, chain integrity) — folded into T-017 escalation wiring

## Decision: Option 2 — Typed DEPENDS_ON query

Each tiered step queries its specific predecessor via `task_relations` with `DEPENDS_ON` type filter. `SplitIntoTieredDAG` already guarantees one-edge-per-step, so the invariant holds: context has no predecessor, decision depends on context, execute depends on decision, verify depends on execute.

Decision step commits its output JSON (`touch_list`, `checks`) to the task's stdout/artifact storage; downstream steps read the prior step's committed artifact via predecessor lookup and typed-DEPENDS_ON traversal.

No schema changes needed; reuses existing task relation infrastructure.
