# Retro: S04 — Tiered pipeline (M3–M5)

| Field | Value |
| --- | --- |
| Sprint | S04-tiered-pipeline |
| Date | 2026-09-19 |

## Went well

- M3 (T-015 DAG splitter, T-016 worker modes) shipped clean in S04's early action and held up under two independent review passes (CodeRabbit + cubic-dev-ai) — the only defects found were the T-019 predecessor-resolution gap, not the splitter/dispatch logic itself.
- T-019 design question resolved without a schema change: typed `DEPENDS_ON` queries via the existing `ListParentTasksByRelation` reuse the one-edge-per-step invariant `SplitIntoTieredDAG` already guarantees.
- Verify-outcome classification (`ClassifyVerifyOutcome`, `VerifyResultOutcome` enum: pass/fail/flake/conflict) is solid, unit-tested, and matches the spec's decision table exactly.
- `docs/tiered-execution.md` M4/M5 sections are a good design reference — the escalation ladder and NEEDS_CONTEXT sequencing are documented at the right level of detail for whoever wires the runtime next.

## Went poorly

**The self-review this retro forced turned up four real gaps that the earlier "T-017/T-018 done" status did not reflect. Recording them here instead of quietly fixing and re-declaring done, per the S02/S03 pattern of surfacing gaps honestly.**

- **Escalation handlers are not wired into the runtime.** `handleVerifyOutcome`, `scheduleMidFix`, `scheduleEscalation`, and `getPredecessorTask` in `escalation.go` are never called from `worker_tiered.go` or anywhere else in production code — confirmed via `grep` across `internal/queue/worker/*.go` excluding tests. The verify step's LLM output (the `VerifyResult` JSON) is never parsed and fed into `ClassifyVerifyOutcome`. Today, a tiered verify failure does **not** trigger mid-fix, escalation, or HUMAN handoff — it just... ends. Unit tests cover the classification function in isolation, which is why `go test` stayed green while the integration was missing.
- **Metadata persistence is a stub, not an implementation.** `getMetadata()` in `escalation.go` unconditionally returns its `defaultVal` and never reads `task.Logs` — despite `setMetadata()` writing there. This means `mid_fix_passes` and `escalate_count` can never accumulate past 1 read, so the "max 2 mid-fix / max 1 escalate" caps described in T-017 would not actually cap anything if the handlers were wired up. This is a correctness bug, not a missing feature.
- **`NEEDS_CONTEXT` is half-added.** The state exists in the Go `TaskState` enum and transition table, but `internal/kanban/db/schema.sql`'s `CHECK (state IN (...))` constraint was never updated to include it. Any code path that tried to persist a task into `NEEDS_CONTEXT` today would fail at the database layer. No pack-rewire, version-bump, or downstream-invalidation logic exists — the state is declared, not implemented.
- **Cost harness numbers are illustrative constants, not measurements.** `tiered-harness.sh` never invokes a mock LLM or reads seeded fixture responses — `BASELINE_TOKENS=13000`, `BASELINE_WALL_TIME=45`, `TIERED_WALL_TIME=28` are hardcoded comments-as-justification, not output from an executed run. T-018 explicitly asked for "mock LLM or seeded responses" with proxy semantics; what shipped is a cost-table calculator over made-up inputs. The 68%/38% figures in the docs are therefore illustrative, not evidence.
- Process regression from S02/S03's own retro action: task files were flipped to `done` in the same commit as the code, but the `done` claim itself was not verified against running code before being written down. "Tests pass" was treated as equivalent to "wired and working," which it wasn't here — the escalation tests test the classifier, not the pipeline.

## Surprises

- The `KanbanStore` interface already had everything needed to wire this correctly (`ListParentTasksByRelation`, `AppendTasksToProject`, `UpdateTaskState` with optimistic concurrency, `PersistTieredDAG`) — the gap is integration work, not missing infrastructure. Closing T-020 (below) should be small.
- Grepping for call sites before writing the retro caught all four gaps in about ten minutes. Cheaper to do at retro time than to have found this after a demo.

## Actions (assign + due)

| Action | Owner | Due |
| --- | --- | --- |
| Correct T-017/T-018 status: keep `done` for the parts that are real (classification, state enum, docs, demo script skeleton), but do not claim "escalation ladder implemented" without the wiring — cross-link to T-020 | sprint owner | S04 close ✅ |
| File T-020 (below) capturing all four gaps as one integration ticket | sprint owner | S04 close ✅ |
| Before marking any future ticket `done`, grep for call sites of new functions, not just `go test` — a passing unit test on an unwired function is not "done" | facilitator | starting S05 |
| Add `NEEDS_CONTEXT` to `schema.sql` CHECK constraint + a migration test that asserts every `models.TaskState` value the Go code considers `Valid()` is also accepted by the DB constraint (prevents this exact class of drift recurring) | sprint owner | S05 |

## Carry into next sprint

- **S05 = tiered pipeline integration** (T-020): wire `handleVerifyOutcome` into the verify step's completion path in `worker_tiered.go`; fix `getMetadata`/`setMetadata` to actually round-trip through `task.Logs`; add `NEEDS_CONTEXT` to the DB schema and implement the pack-rewire/invalidation it requires; replace the cost harness's hardcoded constants with an actual mock-LLM or seeded-fixture run so the token/cost numbers are measured, not asserted.
- Process carry (repeated from S02 and S03, still open): verify "done" against running/wired code, not just green tests, before writing it into a task file.
