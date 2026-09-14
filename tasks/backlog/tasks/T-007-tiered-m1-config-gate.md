# T-007: Tiered M1 — config + complexity gate (no behavior change when off)

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Parent | US-004 |
| Estimate | M |
| Links | [tiered-execution M1](../../../docs/tiered-execution.md) |

## Goal

Add `tiered.enabled` + threshold; default off; below threshold always one-shot.

## Done when

- [ ] Config keys documented in config.reference.yaml
- [ ] Tests: disabled / below / boundary
- [ ] No production path split yet beyond gate plumbing

## Notes

-
