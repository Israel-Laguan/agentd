# US-004: Tiered execution MVP (complex tasks only)

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | ready |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Persona | operator |
| Links | [tiered-execution.md](../../../docs/tiered-execution.md) |

## Story

As an **operator on a hard task**, I want **context→decision→execute→verify with cheap/mid/strong models and a sealed ContextPack**, so that **I spend less money without silent re-gathering.**

## Acceptance criteria

- [ ] Below complexity threshold: identical to one-shot
- [ ] Above threshold: DAG children + ContextPack allowlists
- [ ] Re-gather only via explicit context redo / NEEDS_CONTEXT
- [ ] Escalate → HUMAN without stuck RUNNING

## Notes

Unblocked — SP-002 answers recorded (done 2026-09-14); implementation = M1–M5 in tiered-execution spec.
