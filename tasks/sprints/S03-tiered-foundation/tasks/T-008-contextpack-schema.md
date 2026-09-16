# T-008: Tiered M2 — ContextPack schema + context worker (read-only)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Parent | US-004 |
| Estimate | L |
| PR | **PR-B** — ≤15 files / <800 LOC |
| Links | [tiered-execution M2](../../../docs/tiered-execution.md) |

## Goal

Implement ContextPack schema, size limits, and a context step that writes the pack without write tools.

## Done when

- [x] Schema validated in tests — `internal/queue/worker/contextpack_test.go` (`Validate` version/task_id/summary/paths/excerpts/commands_run cases, duplicate + empty path rejection, counter mismatch)
- [x] Pack stored and referenced from child tasks — `WriteContextPack`/`ReadContextPack` round-trip writes `context_pack.v1.json` in the workspace; `ParentTaskID` links the pack to its parent task
- [x] Broad search allowed only in context step — pack carries `Paths` + `Unknowns`; downstream steps read the sealed pack rather than re-crawling

## Notes

- Surface: `ContextPack` schema + `Validate()` + `EnforceBudget()` + `WriteContextPack`/`ReadContextPack` + `NewContextPack` in `internal/queue/worker/contextpack.go`; budget defaults from `internal/config/tiered.go` (`ContextPackConfig()`).
- Budget enforcement trims optional fields first (`unknowns`, then `constraints`); oversized required content (`summary`/`excerpts`/`commands_run`) fails loudly.
- `ReadContextPack` backfills budget counters that are *absent* from older v1 packs, while preserving explicit zeros so `Validate` still rejects genuine mismatches.
- Shipped under **PR-B** (`feat/s03-tiered-m2-contextpack`); 28 tests, no prod gap found.
