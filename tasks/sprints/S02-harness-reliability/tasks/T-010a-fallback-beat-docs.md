# T-010a: Provider fallback — docs + script (PR-D)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | M |
| PR | **PR-D** — ≤6 files / &lt;350 LOC |
| Links | [PR-PLAN](../PR-PLAN.md), [demo.md](../../../../docs/demo.md) |

## Goal

Document Beat 2 in `docs/harness-reliability.md` — the provider-fallback/breaker mechanism, distinct from `docs/demo.md`'s connector-HUMAN approval demo: (1) two-provider cascade success via secondary; (2) single-entry dead URL → breaker OPEN / HUMAN handoff (breaker state, not the governance loop re-proved in `demo.md`).

## Allowed paths

- `docs/harness-reliability.md`
- `scripts/demo/provider-fallback.sh` (optional)
- `tasks/sprints/S02-harness-reliability/**`

## Done when

- [x] Both scenarios have pass criteria and commands
- [x] Reuses mock/LiteLLM; no billable requirement
- [x] Diff within budget

## Notes

Beat 2 section in `docs/harness-reliability.md` + `scripts/demo/provider-fallback.sh` (prepare-cascade|prepare-breaker|probe-*|status|stop).
