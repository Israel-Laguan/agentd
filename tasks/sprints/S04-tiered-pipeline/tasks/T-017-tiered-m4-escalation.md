# T-017: Tiered M4 — Escalation ladder + NEEDS_CONTEXT

| Field | Value |
| --- | --- |
| Type | task |
| Status | in-progress |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-C** (merged with T-018 per operator decision 2026-09-19; see [PR-PLAN.md](../PR-PLAN.md)) — 20–50 files / <1000 LOC combined |
| Links | [tiered-execution M4](../../../../docs/tiered-execution.md) |

## Goal

Wire the escalation ladder: verify failure → mid fix → verify again → conflict → strong escalate → verify → HUMAN. Also implement `NEEDS_CONTEXT` as a board state that triggers re-gather.

## Done when

- [ ] Verify classifies outcomes: pass, flake (retry), fail (mid fix), conflict (escalate)
- [ ] Mid fix: bounded redo with same pack, then verify again
- [ ] Conflict: one escalate pass (strong model) with pack + failing evidence
- [ ] Still blocked after escalate → HUMAN (existing healing/handoff paths)
- [ ] NEEDS_CONTEXT state: fails the current step, spawns new context child, bumps pack version; new pack path+version propagated to downstream tasks via `DEPENDS_ON` rewire and board pointer update; existing descendants that consumed the old pack are blocked/invalidated and gated on the new context child (cannot run with stale pack)
- [ ] Caps: max mid-fix passes (configurable), max one escalate unless config says otherwise
- [ ] All transitions are durable board states — no stuck RUNNING chat
- [ ] Tests: escalation chain, NEEDS_CONTEXT re-gather with propagation/invalidation, caps enforced, HUMAN fallback

## Notes

- Reuse `targeted_redo.go` ideas for mid-fix
- Reuse existing HUMAN / healing handoff paths for escalation final state
- NEEDS_CONTEXT is an explicit board action — never a silent side quest inside execute
- Propagation: context child writes `context_pack.vN.json`; host updates parent/board pack pointer (version N) and rewires downstream `DEPENDS_ON` to depend on the new context child; tasks already RUNNING with old pack are allowed to finish but their outputs are ignored, while QUEUED/READY downstream are blocked until new pack is ready
