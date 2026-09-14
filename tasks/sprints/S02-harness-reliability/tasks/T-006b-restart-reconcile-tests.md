# T-006b: Restart beat — reconcile tests only (PR-B)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P0 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | L |
| PR | **PR-B** — ≤15 files / &lt;800 LOC |
| Links | [PR-PLAN](../PR-PLAN.md), ghost/stale features under `internal/queue/features/` |

## Goal

Automated coverage that unclean stop + restart cannot leave a silent stuck `RUNNING` (ghost/stale reconcile).

## Allowed paths

- `internal/queue/**/*_test.go`
- `internal/queue/features/*.feature`
- `internal/kanban/**/*_test.go` **only if** asserting store reconcile helpers
- **No** production `.go` in this PR

## Done when

- [ ] Feature or unit tests cover ghost and/or stale reconcile outcomes used by Beat 1
- [ ] Failures name the board state expected
- [ ] If a production bug is found → stop; open **T-006c / PR-C** instead of stuffing a fix here
- [ ] Diff within budget

## Notes

Existing cues: `ghost_reconciliation.feature`, `heartbeat_reconciliation.feature`, `ReconcileGhostTasks` / `ReconcileStaleTasks`.
