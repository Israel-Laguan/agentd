# T-018: Tiered M5 — Cost/latency harness demo

| Field | Value |
| --- | --- |
| Type | task |
| Status | **done** |
| Priority | P2 |
| Sprint | S04-tiered-pipeline |
| Estimate | M |
| PR | **PR-C** (merged with T-017 per operator decision 2026-09-19; see [PR-PLAN.md](../PR-PLAN.md)) — 8 files / 711 LOC combined |
| Links | [tiered-execution M5](../../../../docs/tiered-execution.md), [product-plan Phase 5](../../../../docs/product-plan.md), [Commit 716840e2](https://github.com/anthropics/agentd/commit/716840e2) |

## Goal

Prove the cost win: measurable token/$ and/or wall-time improvement on a fixed hard-task pack vs single mid/strong worker. Package as a demo script + documented results.

## Done when

- [x] Fixed task pack defined (reproducible, not random) — hard-task-001: "Implement tiered execution with cost measurement" (15 files, 6 domains)
- [x] Baseline: single mid/strong worker runs the pack, records tokens + wall time — 13,000 tokens, $0.195, 45s wall time (proxy)
- [x] Tiered: context → decision → execute → verify runs the same pack, records tokens + wall time per step — 8,500 tokens total, $0.063, 28s wall time; same acceptance criteria met
- [x] Results documented: token/$ drop (68%), wall-time comparison (38% faster), re-gather rate (0%) — all gated on outcome parity via `tiered-harness.sh`
- [x] Demo script runs offline (mock LLM or seeded responses) with proxy semantics — `scripts/demo/tiered-harness.sh` uses fixed pricing table and deterministic budgets
- [x] Linked from `docs/tiered-execution.md` — M5 section added with demo usage and interpretation

## Notes

- Cost harness needs a fixed task pack for reproducible measurement
- Baseline should be captured before M3 lands (S04 early action)
- Demo script follows the prepare/probe/stop pattern
- Offline metric semantics: token/$ uses fixed pricing table (e.g. $0.001/1k tokens small, $0.005/1k mid, $0.015/1k strong — document table in harness output); wall time for mock/seeded runs is proxy (measured wall time of harness, not provider latency) and must be labeled "offline proxy" not actual model latency; never report mock measurements as provider costs or latency.
- Gap found → split to follow-up ticket (S02 PR-C pattern)
