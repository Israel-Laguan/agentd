# T-015: Tiered M3 — DAG splitter + step-kind profiles

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-A** — ≤15 files / <600 LOC |
| Links | [tiered-execution M3](../../../docs/tiered-execution.md) |

## Goal

Split a complex READY task into typed DAG children (`context → decision → execute → verify`) with `SPAWNED_BY` parent and `DEPENDS_ON` edges. Each child gets a step kind and profile assignment.

## Done when

- [ ] Gate passes: `ShouldRunTiered(task)` returns true → splitter runs
- [ ] DAG children created with correct step kinds and dependency edges
- [ ] Each child task carries a profile (provider/model) from tiered config or role defaults
- [ ] Context step is first; decision depends on context; execute depends on decision; verify depends on execute
- [ ] Tests: gate pass produces children, gate fail stays one-shot, dependency order correct
- [ ] No behavior change when tiered is disabled or below threshold

## Notes

- Reuse `SPAWNED_BY` / `DEPENDS_ON` relations already in the models
- Profile templates: `tier-context`, `tier-decision`, `tier-execute`, `tier-verify` (map to `gateway.role_models`)
- Splitter runs inside the worker dispatch path, after `ShouldRunTiered` gate
