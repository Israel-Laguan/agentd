# T-015: Tiered M3 — DAG splitter + step-kind profiles

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-A** — ≤15 files / <600 LOC |
| Links | [tiered-execution M3](../../../../docs/tiered-execution.md) |

## Goal

Split a complex READY task into typed DAG children (`context → decision → execute → verify`) with `SPAWNED_BY` parent and `DEPENDS_ON` edges. Each child gets a step kind and profile assignment.

## Done when

- [x] Gate passes: `ShouldRunTiered(task)` returns true → splitter runs — gate shipped in M1/M2 (`internal/queue/worker/phase_splitter.go`); `SplitIntoTieredDAG` constructor written and tested; dispatch wiring landed in T-016
- [x] DAG children created with correct step kinds and dependency edges — `internal/queue/worker/splitter.go` (`SplitIntoTieredDAG`); `TestSplitIntoTieredDAG_StepOrderAndProfiles`, `_DependencyChainIsSequential`
- [x] Each child task carries a profile (provider/model) from tiered config or role defaults — child `AgentID` stamped with its profile template (`tier-context`/`tier-decision`/`tier-execute`/`tier-verify`); resolution to provider/model is existing `AgentProfile`/`gateway.role_models` fallback infra, not re-implemented here
- [x] Context step is first; decision depends on context; execute depends on decision; verify depends on execute — `TestSplitIntoTieredDAG_DependencyChainIsSequential`, `_ContextStartsReady_OthersPending`
- [x] Tests: gate pass produces children, gate fail stays one-shot, dependency order correct — gate->splitter composition not directly tested yet (splitter is unreferenced by design); covered separately by `phase_splitter_test.go`'s `TestShouldRunTiered_*` suite and `splitter_test.go`, composed coverage deferred to T-016 wiring
- [x] No behavior change when tiered is disabled or below threshold — `SplitIntoTieredDAG` is a new, unreferenced pure function; no existing dispatch path calls it, so the one-shot path is untouched regardless of gate state

## Notes

- Reuse `SPAWNED_BY` / `DEPENDS_ON` relations already in the models — confirmed both `models.TaskRelationSpawnedBy` and `models.TaskRelationDependsOn` exist in `internal/models/enums.go` (previously unused in production code); `SplitIntoTieredDAG` is the first caller.
- Step-kind → role mapping (exact): `context` → `gateway.role_models["memory"]` (cheap), `decision` → `gateway.role_models["worker"]` mid-tier, `execute` → `gateway.role_models["worker"]` cheap, `verify` → `gateway.role_models["worker"]` mid-tier, `escalate` → strong `gateway.role_models["worker"]`. Per-role model overrides via `gateway.role_models` take precedence; `tiered.models.<kind>` overrides are not supported by `internal/config.TieredConfig` or `loadTieredConfig`.
- Profile templates: `tier-context`, `tier-decision`, `tier-execute`, `tier-verify` (map to `gateway.role_models` as above)
- T-016 integrated the splitter into the worker dispatch path after the `ShouldRunTiered` gate and owns step-kind dispatch and mode tool allowlists
- **Scope note:** `models.Task`/`models.TaskRelation`/`models.TaskRelationType` actually live in `internal/models/entities.go`/`enums.go` (not `internal/models/task.go` as this ticket's PR-A path list assumed); both relation types already existed unmodified there, so no changes to those files were needed and the allowed-paths mismatch was a non-issue in practice.
- `SplitIntoTieredDAG` is a **pure, in-memory constructor** — no `*Worker` receiver, no store writes. Real persistence of `SPAWNED_BY`/`DEPENDS_ON` children and dispatch wiring landed in T-016 (`PersistTieredDAG` in `internal/kanban/tiered_dag.go`).
- Shipped under **PR-A** (`feat/s04-tiered-m3-splitter`).
