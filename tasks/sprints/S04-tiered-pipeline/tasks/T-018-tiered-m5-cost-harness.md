# T-018: Tiered M5 — Cost/latency harness demo

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P2 |
| Sprint | S04-tiered-pipeline |
| Estimate | M |
| PR | **PR-D** — ≤10 files / <400 LOC |
| Links | [tiered-execution M5](../../../docs/tiered-execution.md), [product-plan Phase 2](../../../../docs/product-plan.md) |

## Goal

Prove the cost win: measurable token/$ and/or wall-time improvement on a fixed hard-task pack vs single mid/strong worker. Package as a demo script + documented results.

## Done when

- [ ] Fixed task pack defined (reproducible, not random)
- [ ] Baseline: single mid/strong worker runs the pack, records tokens + wall time
- [ ] Tiered: context → decision → execute → verify runs the same pack, records tokens + wall time per step
- [ ] Results documented: token/$ drop, wall-time comparison, re-gather rate
- [ ] Demo script runs offline (mock LLM or seeded responses)
- [ ] Linked from `docs/tiered-execution.md`

## Notes

- Cost harness needs a fixed task pack for reproducible measurement
- Baseline should be captured before M3 lands (S04 early action)
- Demo script follows the prepare/probe/stop pattern
- Gap found → split to follow-up ticket (S02 PR-C pattern)
