# T-007: Tiered M1 — config + complexity gate (no behavior change when off)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Parent | US-004 |
| Estimate | M |
| PR | **PR-A** — ≤10 files / <500 LOC |
| Links | [tiered-execution M1](../../../docs/tiered-execution.md) |

## Goal

Add `tiered.enabled` + threshold; default off; below threshold always one-shot.

## Done when

- [x] Config keys documented in config.reference.yaml — `tiered.enabled`, `tiered.complexity_threshold`, `tiered.context_pack.max_paths`, `tiered.context_pack.max_chars` (see `config.reference.yaml` §tiered)
- [x] Tests: disabled / below / boundary — `internal/queue/worker/phase_splitter_test.go` (`TestShouldRunTiered_Disabled`, `_Enabled_BelowThreshold`, `_Enabled_AtThreshold`, `_Enabled_AboveThreshold`, `_ThresholdZero_DisablesSplitting`) and `internal/config/tiered_test.go` (defaults, explicit values, negative-clamp, threshold-zero-disables)
- [x] No production path split yet beyond gate plumbing — `ShouldRunTiered` is the single new surface; `shouldPlan`/one-shot path untouched

## Notes

- Gate is provably inert when off: `tiered.enabled: false` (default) and `complexity_threshold: 0` both `return false` before scoring, so below-threshold ≡ one-shot.
- `ShouldRunTiered` is independent of the existing `shouldPlan` planning gate — enabling tiered does not change plan-phase behaviour.
- Shipped under **PR-A** (`feat/s03-tiered-m1-gate`); 5 files / ~80 LOC.
