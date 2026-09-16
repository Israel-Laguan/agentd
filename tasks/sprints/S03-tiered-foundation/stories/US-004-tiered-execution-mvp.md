# US-004: Tiered execution MVP (complex tasks only)

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | in-progress |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Persona | operator |
| Links | [tiered-execution.md](../../../docs/tiered-execution.md) |

## Story

As an **operator on a hard task**, I want **context→decision→execute→verify with cheap/mid/strong models and a sealed ContextPack**, so that **I spend less money without silent re-gathering.**

## Acceptance criteria

- [x] Below complexity threshold: identical to one-shot — `ShouldRunTiered` returns `false` when `tiered.enabled: false` (default), when `complexity_threshold: 0`, and when the score is below threshold; no existing production path changed (`internal/queue/worker/phase_splitter.go`, T-007)
- [ ] Above threshold: DAG children + ContextPack allowlists — **ContextPack allowlists/schema landed** (`internal/queue/worker/contextpack.go`, T-008); DAG children are **not** in S03 → [T-015](../../S04-tiered-pipeline/tasks/T-015-tiered-m3-splitter.md)/[T-016](../../S04-tiered-pipeline/tasks/T-016-tiered-m3-modes.md)
- [ ] Re-gather only via explicit context redo / NEEDS_CONTEXT — not in S03 → [T-017](../../S04-tiered-pipeline/tasks/T-017-tiered-m4-escalation.md)
- [ ] Escalate → HUMAN without stuck RUNNING — not in S03 → [T-017](../../S04-tiered-pipeline/tasks/T-017-tiered-m4-escalation.md)

## Notes

Unblocked — SP-002 answers recorded (done 2026-09-14); implementation = M1–M5 in tiered-execution spec.

**S03 slice delivered M1 + M2 only** (T-007 config+complexity gate; T-008 ContextPack schema/read+size limits). Per the US-003 precedent (a story is `done` only when its *written* acceptance criteria are met), US-004 stays `in-progress` until M3–M5 land in S04 — the DAG splitter, the worker modes/tool allowlists, the escalation ladder + `NEEDS_CONTEXT`, and the HUMAN handoff.
