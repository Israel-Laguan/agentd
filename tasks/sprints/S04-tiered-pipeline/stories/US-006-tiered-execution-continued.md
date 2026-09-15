# US-006: Tiered execution pipeline (continued)

| Field | Value |
| --- | --- |
| Type | story |
| Status | ready |
| Sprint | S04-tiered-pipeline |
| Parent | product-plan Phase 5 |

## Goal

Complete the tiered execution pipeline: decision → execute → verify DAG with allowlists, escalation ladder + HUMAN handoff, cost/latency harness demo.

## Done when

- [ ] M3: DAG splitter produces typed children with dependency edges
- [ ] M3: Worker modes dispatch correctly per step kind with tool allowlists
- [ ] M4: Escalation ladder degrades to HUMAN without stuck loops
- [ ] M4: NEEDS_CONTEXT triggers re-gather with pack version bump
- [ ] M5: Cost harness shows measurable token/$ improvement vs single-model baseline
- [ ] All milestones have tests + documentation

## Notes

- M1 (gate + config) and M2 (ContextPack schema) completed in S03
- M3/M4 are the hard part — real DAG splitting and worker mode dispatch
- M5 is the proof — without it, the cost wedge is a design bet, not a delivered feature
