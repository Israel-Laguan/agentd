# T-017: Tiered M4 — Escalation ladder + NEEDS_CONTEXT

| Field | Value |
| --- | --- |
| Type | task |
| Status | **done** |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-C** (merged with T-018 per operator decision 2026-09-19; see [PR-PLAN.md](../PR-PLAN.md)) — 8 files / 711 LOC combined |
| Links | [tiered-execution M4](../../../../docs/tiered-execution.md), [Commit 716840e2](https://github.com/anthropics/agentd/commit/716840e2) |

## Goal

Wire the escalation ladder: verify failure → mid fix → verify again → conflict → strong escalate → verify → HUMAN. Also implement `NEEDS_CONTEXT` as a board state that triggers re-gather.

## Done when

- [x] Verify classifies outcomes: pass, flake (retry), fail (mid fix), conflict (escalate) — `ClassifyVerifyOutcome` + `VerifyResultOutcome` enum
- [x] Mid fix: bounded redo with same pack, then verify again — `scheduleMidFix` (max 2 configurable)
- [x] Conflict: one escalate pass (strong model) with pack + failing evidence — `scheduleEscalation` with evidence injection
- [x] Still blocked after escalate → HUMAN (existing healing/handoff paths) — transitions to `TaskStateFailedRequiresHuman`
- [x] NEEDS_CONTEXT state: fails the current step, spawns new context child, bumps pack version — `TaskStateNeedsContext` added to state machine with proper transitions; pack re-versioning via task metadata
- [x] Caps: max mid-fix passes (configurable), max one escalate unless config says otherwise — hardcoded in escalation.go, configurable at deployment
- [x] All transitions are durable board states — no stuck RUNNING chat — uses `UpdateTaskState` with concurrency control
- [x] Tests: escalation chain, NEEDS_CONTEXT re-gather with propagation/invalidation, caps enforced, HUMAN fallback — `escalation_test.go`

## Notes

- Reuse `targeted_redo.go` ideas for mid-fix
- Reuse existing HUMAN / healing handoff paths for escalation final state
- NEEDS_CONTEXT is an explicit board action — never a silent side quest inside execute
- Propagation: context child writes `context_pack.vN.json`; host updates parent/board pack pointer (version N) and rewires downstream `DEPENDS_ON` to depend on the new context child; tasks already RUNNING with old pack are allowed to finish but their outputs are ignored, while QUEUED/READY downstream are blocked until new pack is ready
