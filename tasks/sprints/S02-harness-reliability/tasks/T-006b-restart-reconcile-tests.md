# T-006b: Restart beat — reconcile tests only (PR-B)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
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
- **No** production `.go` in this PR

## Done when

- [x] Feature or unit tests cover ghost and/or stale reconcile outcomes used by Beat 1
- [x] Failures name the board state expected
- [x] No production bug found — T-006c stays backlog
- [x] Diff within budget

## Notes

Existing cues: `ghost_reconciliation.feature`, `heartbeat_reconciliation.feature`, `ReconcileGhostTasks` / `ReconcileStaleTasks`.

Added `internal/queue/features/restart_mid_task.feature` and `internal/queue/restart_mid_task_test.go`. T-006c not needed.
