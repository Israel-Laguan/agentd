# T-052: Golden Goals and Non-Closable Verification Gates

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | internal/queue/worker/worker_goals_agentic_test.go, GUARDRAILS.md |

## Goal

Enforce sticky, non-droppable golden goals and automated verification gates before task completion, inspired by Claudinio Code.

Tasks or user requirements designated as `<goal>…</goal>` cannot be dropped or closed by the worker without passing verified automated quality checks (e.g., test suite / quality check pass). If a worker attempts to finish with an open golden goal or failing verification gate, the run is rejected and looped back to the worker up to a configurable maximum cycle/stall limit, or escalated to a `HUMAN` review state.

## Done when

- [ ] Support marking specific task criteria or goals as "golden" / mandatory.
- [ ] Worker completion guard enforces that golden goals have passing quality checks before transitioning out of IN_PROGRESS.
- [ ] Configurable maximum retry cycles and stall detection for unresolved golden goals before escalating to HUMAN.
- [ ] Unit and integration tests verify that workers cannot prematurely close tasks when verification gates fail.

## Notes

- Claudinio reference: Goals you set stay set. Wrapped in `<goal>…</goal>`, cannot be closed without a passing `run_quality` on the code. Auto-requeued up to Max golden cycles / Max golden stalls.
