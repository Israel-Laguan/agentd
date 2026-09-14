# T-006c: Restart beat — production fix if tests expose a gap (PR-C)

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P0 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | M |
| PR | **PR-C** — ≤10 files / &lt;600 LOC — **only if T-006b fails for a real bug** |
| Links | [PR-PLAN](../PR-PLAN.md) |

## Goal

Minimal fix so restart mid-task meets Beat 1 pass criteria.

## Allowed paths

- `internal/queue/heartbeat_reconcile.go`
- `internal/kanban/tasks_repo.go` (reconcile/claim only)
- Matching tests touched in T-006b

## Done when

- [ ] T-006b scenarios pass
- [ ] No drive-by refactors outside allowed paths
- [ ] Diff within budget

## Notes

Leave `Status: backlog` until T-006b proves a gap.
