# T-007: Tiered M1 — config + complexity gate (no behavior change when off)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Parent | US-004 |
| Estimate | M |
| PR | **PR-A** — ≤10 files / <500 LOC |
| Links | [tiered-execution M1](../../../docs/tiered-execution.md) |

## Goal

Add `tiered.enabled` + threshold; default off; below threshold always one-shot.

## Done when

- [ ] Config keys documented in config.reference.yaml
- [ ] Tests: disabled / below / boundary
- [ ] No production path split yet beyond gate plumbing

## Notes

-
