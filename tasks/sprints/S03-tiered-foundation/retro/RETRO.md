# Retro: S03 — Tiered foundation + close Phase 2

| Field | Value |
| --- | --- |
| Sprint | S03-tiered-foundation |
| Date | 2026-10-28 |

## Went well

- All 4 tickets shipped: T-007 (M1 gate), T-008 (ContextPack M2), T-013 (Beat 2.3), T-014 (Beat 2.4). Phase 2 reliability story is now closed — every beat from restart to disk-watchdog to memory-recall is documented and demoed.
- M1 gate is provably inert when off: tests prove disabled / below-threshold / boundary-zero all stay one-shot. No behavior change in existing worker path — the tiered pipeline builds on solid ground.
- ContextPack schema landed clean: versioned struct, Validate(), EnforceBudget(), WriteContextPack/ReadContextPack round-trip. 28 tests covering schema, size limits, error paths. No prod gap found.
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
| Flip T-007/T-008/T-013/T-014 to `done` in task files | sprint owner | S03 close ✅ |
| US-004 → `in-progress` (not `done`): criteria 3–4 + DAG half of criterion 2 are S04 (T-015–T-017) | sprint owner | S03 close ✅ |
| T-001 → `done` — operator applied About one-liner + topics manually via `gh repo edit` (per user); `gh repo view --json` traceability output not captured here (`gh` unauthenticated) | sprint owner | S03 close ✅ |
| Groom M3-M5 for S04: splitter, worker allowlists, escalation ladder, cost harness | facilitator | before S04 start |
| Capture cost/latency baseline on a fixed task pack before M3 lands | sprint owner | S04 early |

## Close verification (2026-10-28)

Run at S03 close:

- `go test ./...` → EXIT=0; `go vet ./...` → EXIT=0 (clean).
- S03 deliverables in tree: `internal/config/tiered.go` + `internal/queue/worker/phase_splitter.go:ShouldRunTiered`; `internal/queue/worker/contextpack.go` (Validate/EnforceBudget/Read/Write, 28 tests); `internal/queue/disk_watchdog.go` + `disk_watchdog_test.go` + `features/disk_watchdog.feature`; `internal/memory/recall.go` + `recall_test.go` + `recall_*feature`.
- Beats proven offline: `scripts/demo/disk-watchdog.sh` (threshold above free space — no real disk fill), `scripts/demo/memory-recall.sh` (mock LLM + preferences API seeding). `bash -n` clean on all demo scripts (shellcheck not installed locally — noted as a gap).
- `git diff --stat main...HEAD` baseline = 0 (S03 merged); the uncommitted diff at close (67 insertions / 5 files) = backfill-hygiene + demo probe validation, split from the earlier PR-A/PR-B/PR-C/PR-D merges.
- No production gap found by either beat → S02 PR-C "gap → follow-up" branch was a clean skip both times.

### Scope corrections vs retro narrative

- The retro's "went well"/"actions" implied US-004 could be `done` in S03. Per US-003 precedent (a story is `done` only when its *written* criteria are met), US-004's criteria 3–4 and the DAG half of criterion 2 are S04 work (T-015–T-017). US-004 is therefore `in-progress`, not `done`.
- T-001: `gh` was unauthenticated in this session and a public-page HTML scrape did not surface topic tags (likely JS-rendered), so programmatic confirmation was unavailable. Operator confirmed T-001 completed (About set + topics applied manually via `gh repo edit`); marked `done` on operator authority, with traceability via manual application (not the `gh repo view --json` snapshot the task originally asked for).

## Carry into next sprint

- **S04 = tiered M3-M5** (`decision → execute → verify` DAG with allowlists, escalation ladder + HUMAN handoff, cost/latency harness demo) — the hard part of the cost wedge.
- Process carry: flip task statuses in the same commit as the work (S02 + S03 retro action, still open).
