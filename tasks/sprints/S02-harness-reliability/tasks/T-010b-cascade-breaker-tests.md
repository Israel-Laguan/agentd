# T-010b: Provider fallback — cascade/breaker tests (PR-E)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | L |
| PR | **PR-E** — ≤12 files / &lt;700 LOC |
| Links | [PR-PLAN](../PR-PLAN.md), `internal/gateway/routing/`, `internal/queue/safety/` |

## Goal

Tests for cascade-to-secondary and breaker-open / handoff classification — without expanding into a gateway rewrite.

## Allowed paths

- `internal/gateway/**/*_test.go` (+ small fixtures colocated)
- `internal/queue/safety/**/*_test.go`
- Prefer extending existing cascade/breaker tests over new packages

## Done when

- [x] Unreachable primary + healthy secondary → success path covered
- [x] Unreachable-only → breaker failure classification / open path covered
- [x] Diff within budget; no unrelated provider adapter edits

## Notes

Added `beat2_provider_fallback.feature` + `beat2_provider_fallback_test.go` (gateway) and `beat2_breaker_test.go` (queue/safety). Extends existing cascade/breaker coverage; no prod edits.
