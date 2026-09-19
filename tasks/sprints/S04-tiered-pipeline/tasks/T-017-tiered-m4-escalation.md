# T-017: Tiered M4 — Escalation ladder + NEEDS_CONTEXT

| Field | Value |
| --- | --- |
| Type | task |
| Status | **done — classification/state-machine only; runtime wiring is [T-020](../../S05-tiered-integration/tasks/T-020-tiered-verify-wiring-gap.md)** |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-C** (merged with T-018 per operator decision 2026-09-19; see [PR-PLAN.md](../PR-PLAN.md)) — 8 files / 711 LOC combined |
| Links | [tiered-execution M4](../../../../docs/tiered-execution.md), [Commit 716840e2](https://github.com/anthropics/agentd/commit/716840e2), [S04 retro](../retro/RETRO.md) |

## Goal

Wire the escalation ladder: verify failure → mid fix → verify again → conflict → strong escalate → verify → HUMAN. Also implement `NEEDS_CONTEXT` as a board state that triggers re-gather.

## Done when

**Correction (S04 retro, 2026-09-19): the items below marked ⚠️ have the supporting code written but NOT called from the actual tiered dispatch path (`worker_tiered.go`) — confirmed via grep for call sites. Tests pass because they test the functions in isolation, not the pipeline. See [T-020](../../S05-tiered-integration/tasks/T-020-tiered-verify-wiring-gap.md).**

- [x] Verify classifies outcomes: pass, flake (retry), fail (mid fix), conflict (escalate) — `ClassifyVerifyOutcome` + `VerifyResultOutcome` enum (classifier itself is correct and tested; not yet fed by the real verify step's output — T-020)
- [ ] ⚠️ Mid fix: bounded redo with same pack, then verify again — `scheduleMidFix` exists but is unwired, and its cap counter can't accumulate because `getMetadata` is a stub that never reads `task.Logs` (T-020)
- [ ] ⚠️ Conflict: one escalate pass (strong model) with pack + failing evidence — `scheduleEscalation` exists but is unwired (T-020)
- [ ] ⚠️ Still blocked after escalate → HUMAN (existing healing/handoff paths) — code path exists but is unreachable since escalation is never triggered (T-020)
- [ ] ⚠️ NEEDS_CONTEXT state: fails the current step, spawns new context child, bumps pack version — `TaskStateNeedsContext` added to the Go enum/transition table only; **missing from the DB `CHECK` constraint** (persisting it fails today) and no pack-rewire/invalidation logic exists (T-020)
- [ ] ⚠️ Caps: max mid-fix passes (configurable), max one escalate unless config says otherwise — cap values are hardcoded constants that can't actually cap anything without the metadata fix (T-020)
- [x] All transitions are durable board states — no stuck RUNNING chat — `transitionTaskState` uses `UpdateTaskState` with optimistic concurrency (correct once call sites exist)
- [x] Tests: escalation chain, NEEDS_CONTEXT re-gather with propagation/invalidation, caps enforced, HUMAN fallback — `escalation_test.go` covers outcome classification only; no integration test exists yet for the wired pipeline (T-020)

## Notes

- Reuse `targeted_redo.go` ideas for mid-fix
- Reuse existing HUMAN / healing handoff paths for escalation final state
- NEEDS_CONTEXT is an explicit board action — never a silent side quest inside execute
- Propagation: context child writes `context_pack.vN.json`; host updates parent/board pack pointer (version N) and rewires downstream `DEPENDS_ON` to depend on the new context child; tasks already RUNNING with old pack are allowed to finish but their outputs are ignored, while QUEUED/READY downstream are blocked until new pack is ready
