# T-017: Tiered M4 — Escalation ladder + NEEDS_CONTEXT

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | L |
| PR | **PR-C** — ≤15 files / <600 LOC |
| Links | [tiered-execution M4](../../../docs/tiered-execution.md) |

## Goal

Wire the escalation ladder: verify failure → mid fix → verify again → conflict → strong escalate → verify → HUMAN. Also implement `NEEDS_CONTEXT` as a board state that triggers re-gather.

## Done when

- [ ] Verify classifies outcomes: pass, flake (retry), fail (mid fix), conflict (escalate)
- [ ] Mid fix: bounded redo with same pack, then verify again
- [ ] Conflict: one escalate pass (strong model) with pack + failing evidence
- [ ] Still blocked after escalate → HUMAN (existing healing/handoff paths)
- [ ] NEEDS_CONTEXT state: fails the current step, spawns new context child, bumps pack version
- [ ] Caps: max mid-fix passes (configurable), max one escalate unless config says otherwise
- [ ] All transitions are durable board states — no stuck RUNNING chat
- [ ] Tests: escalation chain, NEEDS_CONTEXT re-gather, caps enforced, HUMAN fallback

## Notes

- Reuse `targeted_redo.go` ideas for mid-fix
- Reuse existing HUMAN / healing handoff paths for escalation final state
- NEEDS_CONTEXT is an explicit board action — never a silent side quest inside execute
