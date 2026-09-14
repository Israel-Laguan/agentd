# T-010: Provider fallback beat

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | M |
| Links | [product-plan Phase 2 H2](../../../../docs/product-plan.md), [demo.md](../../../../docs/demo.md), [SP-003](../../S01-positioning-and-demo/spikes/SP-003-demo-path-dry-run.md) |

## Goal

Document and script the provider fallback/beat: killing the primary provider causes cascade via `gateway.order` to continue, or opens a breaker/system HUMAN task — not silent failure.

## Done when

- [ ] `docs/harness-reliability.md` Beat 2 added with pass criteria covering two distinct scenarios:
  - Provider fallback (two-candidate cascade): one unreachable preferred provider + one healthy secondary provider in `gateway.order` → trigger LLM call → assert the request succeeds through the secondary
  - Provider exhaustion (single-entry outage): single-entry `gateway.order` + dead `base_url: http://127.0.0.1:1` (or kill LiteLLM/mock) → generate three unreachable failures → assert breaker `OPEN` or new `HUMAN`/system task

## Notes

Reuse connector-inject recipe from `docs/demo.md:81` / `SP-003`. Prefer mock/LiteLLM to avoid billable calls. Keep `healing.enabled: true`. SP-004 env pre-flight gates `gh` work if T-010 needs PR evidence.
