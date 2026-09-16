# T-014: Beat 2.4 — memory recall on repeat failure (PR-D)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
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

- [x] A repeated failure class retrieves the seeded `{symptom, solution}` via recall/FTS in tests — `internal/memory/recall_test.go` (`TestRetriever_NamespaceIsolation`, `TestRetriever_TimeoutFallback`, `TestRetriever_NilRetriever`, `TestFormatLessons`), features `internal/memory/features/recall_namespace.feature` and `recall_timeout.feature`
- [x] Scenario documented and reproducible offline — `scripts/demo/memory-recall.sh` seeds a `USER_PREFERENCE` `{symptom, solution}` pair through the preferences API (no billable keys, mock provider) and asserts retrieval + `FormatPreferences` rendering
- [x] Linked from `docs/harness-reliability.md` — Beat 2.4 section + helper-script list
- [x] Diff within budget — docs + demo + tests only; **no prod change to recall/librarian paths** (S02 PR-C pattern)

## Notes

Pairs with S02 Beat 1/2: reliability story stays board-visible (recall → task context), never a silent loop.

No production gap found — `internal/memory/recall.go` already satisfied Phase 2.4, so the "gap found → split" branch was not needed.

Shipped under **PR-D** (`docs/s03-memory-recall-beat`). The demo script lagged the rest of PR-D by one commit (retro: "went poorly"); it is now in-tree and syntax-checked.
