# T-020: Wire tiered escalation ladder + fix metadata persistence + NEEDS_CONTEXT schema + real cost measurement

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S05-tiered-integration |
| Estimate | L |
| Links | [S04 retro](../../S04-tiered-pipeline/retro/RETRO.md), [tiered-execution M4/M5](../../../../docs/tiered-execution.md), [T-017](../../S04-tiered-pipeline/tasks/T-017-tiered-m4-escalation.md), [T-018](../../S04-tiered-pipeline/tasks/T-018-tiered-m5-cost-harness.md) |

## Context

S04 shipped `internal/queue/worker/escalation.go` (classification + escalation helper functions) and `scripts/demo/tiered-harness.sh` (cost comparison script), and marked T-017/T-018 `done`. The S04 retro found, via `grep` for call sites and a read of the actual functions, that four things don't work yet despite green tests:

1. **Not wired.** `handleVerifyOutcome`, `scheduleMidFix`, `scheduleEscalation`, `getPredecessorTask` are never called outside `escalation.go` and its test file. The verify step's LLM output (`VerifyResult` JSON) is never parsed or classified in the actual dispatch path (`worker_tiered.go`). A tiered verify failure today does nothing — no mid-fix, no escalation, no HUMAN handoff.
2. **Metadata never persists.** `getMetadata()` always returns `defaultVal` and never reads `task.Logs`, even though `setMetadata()` writes there. `mid_fix_passes` / `escalate_count` cannot accumulate, so caps are unenforceable even once wired.
3. **`NEEDS_CONTEXT` is schema-incomplete.** Present in `models.TaskState` and `validTaskTransitions`, absent from `internal/kanban/db/schema.sql`'s `CHECK (state IN (...))` constraint. Persisting a task into this state today fails at the DB layer.
4. **Cost harness numbers are illustrative, not measured.** `tiered-harness.sh` hardcodes `BASELINE_TOKENS=13000`, `BASELINE_WALL_TIME=45`, `TIERED_WALL_TIME=28` as constants; it never invokes a mock LLM or reads seeded fixtures. T-018 asked for "mock LLM or seeded responses" — what shipped is arithmetic over guessed inputs.

## Done when

- [x] `worker_tiered.go`'s verify step completion path parses the `VerifyResult` JSON output, calls `ClassifyVerifyOutcome`, and calls `handleVerifyOutcome` — a real tiered run demonstrably triggers mid-fix on fail/flake and escalation on conflict (`processTieredVerifyStep`; integration-tested via `w.Process` end-to-end in `worker_tiered_verify_test.go`)
- [x] `getMetadata` actually reads and unmarshals `task.Logs`; round-trip test proves `mid_fix_passes`/`escalate_count` persist and increment across calls (`TestMetadata_RoundTripsThroughTaskLogs`)
- [x] Mid-fix cap (max 2, configurable via `tiered.escalation.max_mid_fix`) and escalate cap (max 1, configurable via `tiered.escalation.max_escalate`) are enforced with the fixed metadata read — test proves the 3rd mid-fix attempt routes to escalation, not another mid-fix (`TestScheduleMidFix_EscalatesOnceCapReached`). Enforcement is backed by a durable count (SPAWNED_BY children of the origin), not an in-memory counter, so it survives across separate worker dispatch cycles.
- [x] `internal/kanban/db/schema.sql` CHECK constraint includes `NEEDS_CONTEXT`; migration v16 rebuilds the `tasks` table (SQLite can't ALTER a CHECK in place) and bumps `currentSchemaVersion` to 16
- [x] Test asserts parity: every `models.TaskState` where `Valid()` is true is also accepted by the DB CHECK constraint (`TestSchemaAcceptsEveryValidTaskState`, prevents this drift recurring — S04 retro action)
- [x] NEEDS_CONTEXT pack-rewire implemented: new context child spawned, pack generation bumped, downstream `DEPENDS_ON` edges rewired to the new context/decision chain, stale-pack `READY` descendants blocked and later re-readied once the new decision completes (`handleNeedsContext` + `reconcileBlockedDependents`; new `KanbanStore.SpawnTieredContinuation`/`RewireDependsOn` methods, store-level and worker-level tests both green)
- [x] `tiered-harness.sh` replaced with a version that reads seeded fixture responses per step (`scripts/demo/fixtures/*.json`) and reports token/cost/time totals derived from that run — hardcoded constants (`BASELINE_TOKENS`, `BASELINE_WALL_TIME`, `TIERED_WALL_TIME`) removed
- [x] `docs/tiered-execution.md` M4/M5 sections updated to reflect the actual wiring and the fixture-derived harness numbers, clearly labeled mock/offline throughout

## Notes

- This ticket exists because "tests pass" was treated as equivalent to "wired and working" in S04 — the escalation tests validate the classifier in isolation, not the pipeline. Verify wiring by grepping for call sites, not just running `go test`, before closing this ticket.
- Reuse existing store methods — no new `KanbanStore` interface methods should be needed: `ListParentTasksByRelation`, `AppendTasksToProject`, `UpdateTaskState` (optimistic concurrency via `expectedUpdatedAt`), `PersistTieredDAG`.
