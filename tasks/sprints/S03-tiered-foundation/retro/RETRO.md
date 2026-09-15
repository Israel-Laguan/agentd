# Retro: S03 — Tiered foundation + close Phase 2

| Field | Value |
| --- | --- |
| Sprint | S03-tiered-foundation |
| Date | 2026-09-15 |

## Went well

- All 4 tickets shipped: T-007 (M1 gate), T-008 (ContextPack M2), T-013 (Beat 2.3), T-014 (Beat 2.4). Phase 2 reliability story is now closed — every beat from restart to disk-watchdog to memory-recall is documented and demoed.
- M1 gate is provably inert when off: tests prove disabled / below-threshold / boundary-zero all stay one-shot. No behavior change in existing worker path — the tiered pipeline builds on solid ground.
- ContextPack schema landed clean: versioned struct, Validate(), EnforceBudget(), WriteContextPack/ReadContextPack round-trip. 22 tests covering schema, size limits, error paths. No prod gap found.
- Beat demos follow the established pattern (prepare/probe/stop) with fault injection that never touches a real disk or live LLM. Shell-metacharacter validation included from the start (S02 retro action applied).
- PR budgets held: 13 files / ~1131 insertions across 3 commits. No mixing of tiered runtime + beat docs + prod fixes.
- Full test suite passes (`go test ./...` clean, `go vet` clean).

## Went poorly

- T-014 (memory-recall.sh) was not committed with the rest of the PR-D work — needed a follow-up prompt. Status hygiene still lags the work (task files not flipped to `done` in the same commit).
- No cost/latency baseline captured for M1/M2 — we can't yet measure the token/$ improvement that M3-M5 promises. Need a harness before the pipeline gets complex.

## Surprises

- The existing disk watchdog + recall implementations needed zero production changes — both beats were purely prove-and-document. The S02 "gap found → split" pattern resolved to a clean skip both times.
- ContextPack budget enforcement was simpler than expected: trim unknowns first, then constraints. No overflow edge cases required creative solutions.
- The `shouldPlan` / `ShouldRunTiered` separation worked cleanly — two independent gates, no coupling. The tiered gate does not interfere with the existing planning gate.

## Actions (assign + due)

| Action | Owner | Due |
| --- | --- | --- |
| Flip T-007/T-008/T-013/T-014 + US-004 to `done` in task files | sprint owner | S03 close |
| Groom M3-M5 for S04: splitter, worker allowlists, escalation ladder, cost harness | facilitator | before S04 start |
| Capture cost/latency baseline on a fixed task pack before M3 lands | sprint owner | S04 early |

## Carry into next sprint

- **S04 = tiered M3-M5** (`decision → execute → verify` DAG with allowlists, escalation ladder + HUMAN handoff, cost/latency harness demo) — the hard part of the cost wedge.
- Process carry: flip task statuses in the same commit as the work (S02 + S03 retro action, still open).
