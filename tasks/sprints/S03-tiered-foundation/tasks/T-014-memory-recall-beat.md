# T-014: Beat 2.4 — memory recall on repeat failure (PR-D)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P2 |
| Sprint | S03-tiered-foundation |
| Parent | US-003 (close Phase 2) |
| Estimate | M |
| PR | **PR-D** — ≤10 files / <600 LOC |
| Links | [product-plan Phase 2.4](../../../../docs/product-plan.md), [harness-reliability.md](../../../../docs/harness-reliability.md) |

## Goal

Prove product-plan Phase 2.4: on a repeated failure class, Librarian/FTS surfaces a prior `{symptom, solution}` instead of re-burning tokens. Docs + reproducible scenario + tests.

## Allowed paths

- `docs/harness-reliability.md` (Beat 2.4 section only)
- `internal/memory/**` recall/librarian `*_test.go`, `features/*.feature` + step files (tests only)
- `scripts/demo/memory-recall.sh` (new, optional — seed a `{symptom, solution}` pair via fixtures, not live curation)
- `tasks/sprints/S03-*/**`

## Explicitly excluded

- Prod changes to recall/librarian paths — gap found → split to a follow-up ticket (S02 PR-C pattern)
- Dream consolidation tuning, embedding-model swaps

## Done when

- [ ] A repeated failure class retrieves the seeded `{symptom, solution}` via recall/FTS in tests
- [ ] Scenario documented and reproducible offline
- [ ] Linked from `docs/harness-reliability.md`
- [ ] Diff within budget

## Notes

Pairs with S02 Beat 1/2: reliability story stays board-visible (recall → task context), never a silent loop.
