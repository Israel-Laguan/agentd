# T-020: Wire tiered escalation ladder + fix metadata persistence + NEEDS_CONTEXT schema + real cost measurement

| Field | Value |
| --- | --- |
| Type | task |
| Status | review |
| Priority | P1 |
| Sprint | S05-tiered-integration |
| Estimate | L |
| Links | [S04 retro](../../S04-tiered-pipeline/retro/RETRO.md), [tiered-execution M4/M5](../../../../docs/tiered-execution.md), [T-017](../../S04-tiered-pipeline/tasks/T-017-tiered-m4-escalation.md), [T-018](../../S04-tiered-pipeline/tasks/T-018-tiered-m5-cost-harness.md) |

## Context

S04 shipped `internal/queue/worker/escalation.go` (classification + escalation helper functions) and `scripts/demo/tiered-harness.sh` (cost comparison script), and marked T-017/T-018 `done`. The S04 retro found, via `grep` for call sites and a read of the actual functions, that four things don't work yet despite green tests:

1. **Not wired.** `handleVerifyOutcome`, `scheduleMidFix`, `scheduleEscalation`, `getPredecessorTask` are never called outside `escalation.go` and its test file. The verify step's LLM output (`VerifyResult` JSON) is never parsed or classified in the actual dispatch path (`worker_tiered.go`). A tiered verify failure today does nothing — no mid-fix, no escalation, no HUMAN handoff.
2. **Metadata never persists.** `getMetadata()` always returns `defaultVal` and never reads `task.Logs`, even though `setMetadata()` writes there. `mid_fix_passes` / `escalate_count` cannot accumulate, so caps are unenforceable even once wired.
3. **`NEEDS_CONTEXT` is schema-incomplete.** Present in `models.TaskState` and `validTaskTransitions`, absent from `internal/kanban/db/schema.sql`'s `CHECK (state IN (...))` constraint. Persisting a task into this state today fails at the DB layer.
4. **Cost harness numbers are illustrative, not measured.** `tiered-harness.sh` hardcodes `BASELINE_TOKENS=13000`, `BASELINE_WALL_TIME=45`, `TIERED_WALL_TIME=28` as constants; it never invokes a mock LLM or reads seeded fixtures. T-018 asked for "mock LLM or seeded responses" — what shipped is arithmetic over guessed inputs.

## Done when

- [x] `worker_tiered.go`'s verify step completion path parses the `VerifyResult` JSON output, calls `ClassifyVerifyOutcome`, and calls `handleVerifyOutcome` — `processTieredVerifyStep` (`worker_tiered_verify.go`) intercepts the verify step, reads the committed `RESULT` event, classifies it, and routes into the ladder. Call sites verified by grep, not just by green tests
- [x] ~~`getMetadata` actually reads and unmarshals `task.Logs`~~ — **superseded**: `task.Logs` has no store setter (`KanbanStore` exposes no Logs write), so a Logs-backed counter was unimplementable without a new interface method this ticket forbids. Counters are now derived from the steps on the board instead — `countTieredSteps` counts verify/escalate children — which is restart-safe and needs no new storage. The dead `getMetadata`/`setMetadata`/`getDecisionArtifact` scaffolding was deleted
- [x] Mid-fix cap (max 2) and escalate cap (max 1) are enforced — `TestScheduleMidFix_CapRoutesToEscalation` proves the 3rd attempt routes to escalation, `TestScheduleEscalation_CapHandsOffToHuman` proves exhaustion lands on `FAILED_REQUIRES_HUMAN`. Both caps are constants, not config keys (noted in the spec)
- [x] The origin resolves through the supported path — `PersistTieredDAG` leaves it `BLOCKED` and `BLOCKED → COMPLETED` is illegal, so `tryResolveTieredOrigin` records the verdict via `UpdateTaskResult` after the store unblocks it. This also closes the re-split loop the wiring would otherwise have created (a resolved origin returning to `READY` was previously eligible for `ShouldRunTiered` again)
- [x] `internal/kanban/db/schema.sql` CHECK constraint includes `NEEDS_CONTEXT`, and schema **v16** (`migrations/task_states_needs_context.go`) rebuilds the tasks table to widen the constraint on existing databases. `TestMigrationV16AddsNeedsContextState` proves a pre-v16 DB rejects the state, accepts it after `Run`, keeps its rows and all four indexes, and still rejects an unknown state; `TestMigrationV16IsIdempotent` covers the re-run path
- [x] Parity test landed: `internal/kanban.TestTaskStateCheckConstraintParity` inserts every `models.AllTaskStates` value against the real schema, and `TestTaskStateCheckRejectsUnknownState` covers the other direction. `Valid()` is now derived from `AllTaskStates` so the enum and the test cannot drift. Mutation-checked: adding a bogus state to the list fails the test
- [x] NEEDS_CONTEXT re-gather implemented (`worker_tiered_regather.go`), with two deliberate deviations from the original wording, both documented in the spec:
  - **New context child spawned** ✓ — a whole fresh `context → decision → execute → verify` chain, carrying the stated reason into the new context step.
  - **Stale descendants blocked** ✓ — `PENDING`/`READY`/`QUEUED` siblings are parked in `NEEDS_CONTEXT`; `RUNNING` ones finish and are superseded.
  - **`DEPENDS_ON` edges rewired** — *not done, by design.* Rewiring needs a relation-mutation store method this ticket forbids. The new chain depends on the new context step by construction, which reaches the same end state; the abandoned steps stay visible on the board.
  - **Pack version bumped** — *not done, deliberately.* `ContextPackVersion` is the **schema** version and `Validate()` rejects anything else, so `context_pack.v2.json` would assert "schema v2", not "second attempt". The re-gather rewrites the pack in place; the generation is visible as a second context step. Real generation numbering needs a field in the pack format — **follow-up, not silently dropped**
- [x] `tiered-harness.sh` rewritten against seeded fixtures in `scripts/demo/fixtures/tiered/` (task pack, per-step responses, baseline response). Token counts come from actual fixture byte size at a documented 4-bytes/token proxy; costs from the pricing table. `BASELINE_TOKENS`/`BASELINE_WALL_TIME`/`TIERED_WALL_TIME` are gone — edit a fixture and the numbers move. The harness also reads the acceptance verdict from the seeded verify fixture and exits non-zero if the tiered arm did not pass, so both arms are held to the same criterion
- [x] **Time totals deliberately not reported.** Reading fixtures takes microseconds and says nothing about provider latency. Rather than substitute one fabricated latency number for another, the harness prints no latency and says why
- [x] `docs/tiered-execution.md` M4/M5 updated to the fixture-derived numbers, including the correction below

## Notes

- This ticket exists because "tests pass" was treated as equivalent to "wired and working" in S04 — the escalation tests validate the classifier in isolation, not the pipeline. Verify wiring by grepping for call sites, not just running `go test`, before closing this ticket.
- **Wiring pass (2026-09-19):** the ladder is reachable from `Worker.Process`:
  `Process → tryDispatchTieredStep → processTieredStep → processTieredVerifyStep → readVerifyResult → ClassifyVerifyOutcome → handleVerifyOutcome → scheduleMidFix/scheduleEscalation`,
  and `Process → tryTieredOrigin → tryResolveTieredOrigin` for the origin. Confirmed by grepping non-test call sites. The new tests were mutation-checked (disabling mid-fix scheduling, and completing the origin regardless of verdict) and both mutants failed the suite, so the coverage is not vacuous.
- A verify step that returns a terminal-success LLM answer still commits a *successful* task result before classification runs — that is why a non-pass verdict must append a redo rung (which re-blocks the origin) rather than relying on the step's own result to signal failure.
- **The measured result contradicts what S04 claimed.** The old table asserted a *34% token reduction*. Measured against the fixtures, the tiered pipeline spends **2.88x MORE tokens** (947 -> 2,728) and still costs **43% less** ($0.0142 -> $0.0081), because the two largest steps run on the small model. Tiered execution is a price-per-token play, not a token-efficiency play — the token row is a cost the design pays, not a benefit. The spec now says so explicitly; the old 34%/68%/88% figures were never measured.
- All five `tier-*` profiles are now seeded by `agentd init` (`cmd/agentd/profiles.go`) with empty provider/model so `gateway.role_models` still picks the tier models. `TestSeededProfilesCoverTieredSteps` fails if a step kind is added without a profile, which closes the `ErrAgentProfileNotFound` caveat raised in the previous pass.
- `tier-escalate` is now a registered step profile with its own allowlist and prompt. Like the other `tier-*` profiles it must be seeded for escalation to dispatch; an unseeded profile fails loudly with `ErrAgentProfileNotFound` rather than silently skipping the rung.
- Reuse existing store methods — no new `KanbanStore` interface methods should be needed: `ListParentTasksByRelation`, `AppendTasksToProject`, `UpdateTaskState` (optimistic concurrency via `expectedUpdatedAt`), `PersistTieredDAG`.
