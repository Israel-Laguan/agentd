# T-010a: Provider fallback — docs + script (PR-D)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | M |
| PR | **PR-D** — ≤6 files / &lt;350 LOC |
| Links | [PR-PLAN](../PR-PLAN.md), [demo.md](../../../../docs/demo.md) |

## Goal

Document Beat 2: (1) two-provider cascade success via secondary; (2) single-entry dead URL → breaker OPEN / HUMAN.

## Allowed paths

- `docs/harness-reliability.md`
- `scripts/demo/provider-fallback.sh` (optional)
- `tasks/sprints/S02-harness-reliability/**`

## Done when

- [ ] Both scenarios have pass criteria and commands
- [ ] Reuses mock/LiteLLM; no billable requirement
- [ ] Diff within budget
