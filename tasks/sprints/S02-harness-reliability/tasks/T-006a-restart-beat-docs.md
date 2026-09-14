# T-006a: Restart beat — docs + operator script (PR-A)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P0 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | M |
| PR | **PR-A** — ≤8 files / &lt;400 LOC |
| Links | [PR-PLAN](../PR-PLAN.md), [SP-001](../../S01-positioning-and-demo/spikes/SP-001-reliability-beat.md) |

## Goal

Make Beat 1 (restart mid-task) runnable from docs without code changes.

## Allowed paths

- `docs/harness-reliability.md`
- `scripts/demo/restart-mid-task.sh` (optional)
- `README.md` (one link) or `docs/demo.md` (link only)
- `tasks/sprints/S02-*/**` status updates

## Done when

- [x] Pass criteria written (no silent stuck `RUNNING`; same `--home`)
- [x] Exact kill/restart/`curl` sequence
- [x] Points to existing reconcile behavior; does not re-prove connector HUMAN (`docs/demo.md`)
- [x] `git diff --stat` within PR-A budget

## Notes

Gate with SP-004 only if you need `gh` for the PR itself.
