# SP-002: Lock tiered-execution open questions

| Field | Value |
| --- | --- |
| Type | spike |
| Status | ready |
| Priority | P2 |
| Sprint | S01-positioning-and-demo |
| Time box | 0.5 day |
| Links | [tiered-execution.md](../../../docs/tiered-execution.md) |

## Question

Decide defaults for: complexity threshold, ContextPack storage (file vs SQLite vs both), whether verify owns test selection, and how agentic mode attaches to the execute step.

## Output

- [ ] Answers written into `docs/tiered-execution.md` Open questions section (resolved)
- [ ] Explicit: complexity gate remains hard; no runtime work in S01

## Out of scope

Implementing M1–M5.

## Notes

Complexity gate is already agreed: simple = one-shot; pipeline only above threshold.
