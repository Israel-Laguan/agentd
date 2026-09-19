# Retro: S05 — Tiered pipeline integration

| Field | Value |
| --- | --- |
| Sprint | S05-tiered-integration |
| Date | 2026-09-19 |

## Went well

- The S04 retro's own diagnosis was accurate and made this sprint fast: all four gaps it named (verify wiring, metadata stub, missing `NEEDS_CONTEXT` CHECK entry, illustrative harness constants) were exactly the four things that needed fixing, with no surprise fifth gap once work started.
- The existing `KanbanStore` interface really did cover the wiring: `ListParentTasksByRelation`, `AppendTasksToProject`, `UpdateTaskState`, `PersistTieredDAG` were enough for the escalation ladder itself. Two small additions were still needed — see "Went poorly."
- Tracing the actual commit path (`CommitTextWithProfile` → `commitSucceeded` → `AppendTaskResultEvent`) before writing the verify-parsing code caught that the model's final text lives in a `RESULT` event, not `task.Description` — avoided repeating the same mistake `parseAndConfigurePack`'s context-step code already made (it reads `committed.Description`, which nothing ever populates; left alone, out of scope for T-020).
- Every wiring claim in this sprint has an integration test backing it that exercises the real dispatch path (`w.Process` → `tryDispatchTieredStep` → `processTieredStep`), not just the classifier/helper functions in isolation — directly addressing the S04 retro's "tests pass ≠ wired" finding.

## Went poorly / had to improvise beyond the ticket's literal scope

- **`AppendTasksToProject` does not create a `SPAWNED_BY` relation, despite `escalation.go`'s own comment claiming it does.** It inserts a `BLOCKS` edge (`InsertTaskRelation` in `materialize_insert.go`). Since `tryDispatchTieredStep` resolves a tiered step's origin strictly via `SPAWNED_BY`, mid-fix/escalate/re-gather tasks created that way would never dispatch as tiered steps at all. Fixing `AppendTasksToProject` itself was too risky (other callers depend on its `BLOCKS` semantics for the generic child-unlock mechanism). Added two new, narrowly-scoped `KanbanStore` methods instead: `SpawnTieredContinuation` (SPAWNED_BY + optional DEPENDS_ON onto an already-BLOCKED origin — `PersistTieredDAG` requires RUNNING/READY, which the origin no longer is by mid-fix time) and `RewireDependsOn` (redirect a DEPENDS_ON edge, used by NEEDS_CONTEXT). This is the one place this sprint went beyond "no new interface methods should be needed" — recorded here rather than silently expanding scope.
- **`task.Logs` has no backing DB column and is never populated on load.** The metadata fix as literally specified (fix `getMetadata` to read `task.Logs`) is real and tested, but on its own doesn't survive a task reload — mid-fix/escalate counts are instead derived durably by counting persisted `SPAWNED_BY` children, then routed through `getMetadata`/`setMetadata` so the two agree. Worth deciding, in a future sprint, whether `task.Logs` should get a real column or be retired as a metadata API.
- **NEEDS_CONTEXT can only be triggered from the decision step**, not execute/verify, and only via an explicit `{"needs_context": true, ...}` sentinel the decision prompt now documents. A more general trigger (any step, mid-run) would need the agentic engine to support intercepting a result before commit, which today it doesn't — committing happens unconditionally inside `engine.Process` before the tiered-specific code regains control. Scoping the trigger to decision-only avoided a much larger engine change; flagging it as a real limitation rather than full parity with T-017's original wording ("if decision/execute/verify step determines...").
- Escalation is now a genuinely one-off step (`tier-escalate`, not a redo of one of the four fixed DAG kinds) with its own tool allowlist/prompt — this diverged from literally reusing `AppendTasksToProject`'s draft-task shape, since the escalate task needed a real AgentID for the tiered dispatcher to route it correctly.

## Surprises

- `parseAndConfigurePack` (context step, shipped in T-017) reads `committed.Description` for the pack JSON, which is never populated by any commit path in prod or test — a second, separate "shipped but not wired" bug, out of scope for this ticket. Left as-is; worth its own ticket.
- The DB CHECK constraint fix required a full table rebuild (SQLite can't ALTER a CHECK constraint in place) — same shape as the v2/v4 migrations that added `BLOCKED`/`FAILED_REQUIRES_HUMAN` earlier, so the pattern was already established; just needed the current (v15) column set carried forward correctly.

## Actions

- Decide `task.Logs`: add a real `logs` column, or drop the field and the `getMetadata`/`setMetadata` API in favor of the durable-count approach used everywhere it actually matters. (Owner: unassigned: next sprint planning)
- File a follow-up ticket for the context step's `committed.Description` bug found above — same "not wired" pattern as this sprint's original bugs, in a part of the code this ticket didn't touch. (Owner: unassigned)
- If a NEEDS_CONTEXT trigger from execute/verify (not just decision) is wanted, it needs an agentic-engine change (pre-commit interception) — scope that explicitly before starting, it's bigger than this sprint's other work. (Owner: unassigned)
