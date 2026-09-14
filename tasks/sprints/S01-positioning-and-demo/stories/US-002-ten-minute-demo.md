# US-002: 10-minute governance demo

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | done |
| Priority | P0 |
| Sprint | S01-positioning-and-demo |
| Persona | operator |
| Links | [product-plan Phase 1](../../../docs/product-plan.md) |

## Story

As an **operator**, I want **a scripted path from ask → approve → board → HUMAN handoff**, so that **I can show governance, not “it wrote an API.”**

## Acceptance criteria

- [x] `docs/demo.md` lists exact commands and expected board states — `docs/demo.md:1`
- [x] Path includes human plan approval and at least one permission/HUMAN beat — connector inject `BLOCKED + Manual review: AI providers unavailable` (SP-003 GO)
- [x] README Quickstart links the 10-minute path — `README.md:37`
- [x] Cold run by someone familiar with Go tooling finishes in ~10 minutes (or documents blockers) — SP-003 run log `spikes/SP-003-demo-path-dry-run.md:100` passes

## Notes

- Children: T-004, T-005; related spike SP-001
