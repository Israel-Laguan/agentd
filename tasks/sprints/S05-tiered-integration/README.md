# Sprint S05 — Tiered pipeline integration

| Field | Value |
| --- | --- |
| Window | _TBD_ |
| Goal | Close the gap between S04's escalation/cost-harness primitives and actual runtime behavior — wire, persist, migrate, measure |
| Status | planned |
| Based on | [S04 retro](../S04-tiered-pipeline/retro/RETRO.md) |

## Goal

S04 shipped the *pieces* of M4 (escalation ladder) and M5 (cost harness) — classification logic, a state enum, a demo script skeleton, docs — but the S04 retro found none of it is actually wired into the running tiered pipeline. S05 closes that gap.

## In scope

| ID | Type | Title | Status | Priority |
| --- | --- | --- | --- | --- |
| [T-020](tasks/T-020-tiered-verify-wiring-gap.md) | task | Wire escalation ladder into verify dispatch + fix metadata persistence + NEEDS_CONTEXT schema + real cost measurement | ready | P1 |

## Explicitly out of scope

- New step kinds or model tiers beyond context/decision/execute/verify/escalate
- UI for the escalation ladder or cost harness results
- Real (non-mock) LLM cost harness runs against a live provider

## Risks / dependencies

- `getMetadata`/`setMetadata` fix touches every caller in `escalation.go` — verify no other code path assumed the (currently broken) always-default behavior.
- `NEEDS_CONTEXT` schema migration must not break existing rows or the `Valid()`/DB constraint parity check added as an S04 retro action.

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at close.

## Links

- [S04 retro](../S04-tiered-pipeline/retro/RETRO.md)
- [tiered-execution spec](../../../docs/tiered-execution.md)
