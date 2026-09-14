# SP-002: Lock tiered-execution open questions

| Field | Value |
| --- | --- |
| Type | spike |
| Status | done |
| Priority | P2 |
| Sprint | S01-positioning-and-demo |
| Time box | 0.5 day |
| Links | [tiered-execution.md](../../../../docs/tiered-execution.md) |

## Question

Decide defaults for: complexity threshold, ContextPack storage (file vs SQLite vs both), whether verify owns test selection, and how agentic mode attaches to the execute step.

## Decisions (2026-09-14)

| Topic | Decision |
| --- | --- |
| **Complexity gate** | Hard rule unchanged: below threshold = one-shot. Default `tiered.complexity_threshold` aligned with planning spirit: **reuse `EstimateTaskComplexity` score; ship default `200`** (tune via config). `0` or `tiered.enabled: false` = off. Frontdesk may set per-task override later; **v1 = config only**. |
| **ContextPack storage** | **Workspace file primary** (`context_pack.vN.json` under project workspace) + **board pointer** (task comment or task field with path + version). Not SQLite blob in v1 (easier to inspect/diff). |
| **Verify owns tests?** | **No for v1** — verify runs **decision-specified** checks only (commands/paths listed in the decision artifact). Verify may *classify* fail vs flake vs conflict; it does not invent a new test suite. |
| **Agentic on execute** | Execute profile **may** set `AgenticMode: true` when the provider supports tools. Context + decision stay structured JSON / no broad tools (context: read-only gather; decision: pack-bounded). Escalate uses strong model, still pack-bounded. |

Complexity gate remains hard; **no runtime work in S01** — these answers unlock backlog T-007 (M1).

## Output

- [x] Answers written into `docs/tiered-execution.md` Open questions section
- [x] Explicit: complexity gate remains hard; no runtime work in S01

## Out of scope

Implementing M1–M5.

## Notes

-
