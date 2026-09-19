# T-018: Tiered M5 — Cost/latency harness demo

| Field | Value |
| --- | --- |
| Type | task |
| Status | **done — script skeleton + docs only; numbers are illustrative, not measured. Real measurement is [T-020](../../S05-tiered-integration/tasks/T-020-tiered-verify-wiring-gap.md)** |
| Priority | P2 |
| Sprint | S04-tiered-pipeline |
| Estimate | M |
| PR | **PR-C** (merged with T-017 per operator decision 2026-09-19; see [PR-PLAN.md](../PR-PLAN.md)) — 8 files / 711 LOC combined |
| Links | [tiered-execution M5](../../../../docs/tiered-execution.md), [product-plan Phase 5](../../../../docs/product-plan.md), [Commit 716840e2](https://github.com/anthropics/agentd/commit/716840e2), [S04 retro](../retro/RETRO.md) |

## Goal

Prove the cost win: measurable token/$ and/or wall-time improvement on a fixed hard-task pack vs single mid/strong worker. Package as a demo script + documented results.

## Done when

**Correction (S04 retro, 2026-09-19): `tiered-harness.sh` never invokes a mock LLM or reads seeded fixtures — it calculates cost/time from hardcoded constants (`BASELINE_TOKENS=13000`, `BASELINE_WALL_TIME=45`, `TIERED_WALL_TIME=28`). The 68%/38% figures below are illustrative, not evidence. See [T-020](../../S05-tiered-integration/tasks/T-020-tiered-verify-wiring-gap.md).**

- [x] Fixed task pack defined (reproducible, not random) — hard-task-001: "Implement tiered execution with cost measurement" (3 domains: worker, models, config, not 6)
- [ ] ⚠️ Baseline: single mid/strong worker runs the pack, records tokens + wall time — 13,000 tokens / $0.195 / 45s are hardcoded, not recorded from an actual run (T-020)
- [ ] ⚠️ Tiered: context → decision → execute → verify runs the same pack, records tokens + wall time per step — 8,500 tokens / $0.0225 / 28s are hardcoded, not recorded from an actual run (T-020); cost corrected from $0.063 to match calculated $0.0225
- [ ] ⚠️ Results documented: token/$ drop (34% tokens / 88% cost), wall-time comparison (38% faster), re-gather rate (0%) — documented, but derived from illustrative constants, not a gated real-outcome comparison (T-020)
- [ ] ⚠️ Demo script runs offline (mock LLM or seeded responses) with proxy semantics — script runs offline, but has no mock LLM or seeded-response mechanism at all; it never simulates a model call (T-020)
- [x] Linked from `docs/tiered-execution.md` — M5 section added with demo usage and interpretation (interpretation needs updating once T-020 lands real numbers)

## Notes

- Cost harness needs a fixed task pack for reproducible measurement
- Baseline should be captured before M3 lands (S04 early action)
- Demo script follows the prepare/probe/stop pattern
- Offline metric semantics: token/$ uses fixed pricing table (e.g. $0.001/1k tokens small, $0.005/1k mid, $0.015/1k strong — document table in harness output); wall time for mock/seeded runs is proxy (measured wall time of harness, not provider latency) and must be labeled "offline proxy" not actual model latency; never report mock measurements as provider costs or latency.
- Gap found → split to follow-up ticket (S02 PR-C pattern)
