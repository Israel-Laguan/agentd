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

- [x] M3: DAG splitter produces typed children with dependency edges
- [x] M3: Worker modes dispatch correctly per step kind with tool allowlists
- [ ] M4: Escalation ladder degrades to HUMAN without stuck loops — classifier + state machine shipped in S04; runtime wiring carried to S05 T-020 (see [S04 retro](../retro/RETRO.md))
- [ ] M4: NEEDS_CONTEXT triggers re-gather with pack version bump — enum-only in S04, missing DB constraint + rewire logic; S05 T-020
- [ ] M5: Cost harness shows measurable token/$ improvement vs single-model baseline — S04 script uses illustrative constants, not measured runs; S05 T-020
- [ ] All milestones have tests + documentation — M3 fully covered; M4/M5 covered at the unit level only, integration tests pending S05

## Notes

- M1 (gate + config) and M2 (ContextPack schema) completed in S03
- M3/M4 are the hard part — real DAG splitting and worker mode dispatch
- M5 is the proof — without it, the cost wedge is a design bet, not a delivered feature
