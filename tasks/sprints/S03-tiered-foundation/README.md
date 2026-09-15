# Sprint S03 — Tiered foundation + close Phase 2

| Field | Value |
| --- | --- |
| Window | 2026-10-14 → 2026-10-28 |
| Goal | Land tiered execution M1–M2 (no behavior change when off) and close the Phase 2 reliability story with disk-watchdog and memory-recall beats |
| Status | planned |
| Based on | [S02 retro](../S02-harness-reliability/retro/RETRO.md) |

## Goal

Start the cost wedge (product-plan Phase 5, milestones M1–M2) on top of the S02 reliability floor, and finish product-plan Phase 2 (2.3–2.4) so every reliability beat is demoed. Audience: operator on hard tasks (tiered) + adopter evaluating unattended runs (beats).

**PR discipline:** see [`PR-PLAN.md`](PR-PLAN.md) — group tickets by allowed paths; measure with `git diff --stat main...HEAD` before opening a PR. Prod fixes split out of beat PRs (S02 rule).

## In scope

| ID | Type | Title | PR | Status | Priority |
| --- | --- | --- | --- | --- | --- |
| [US-004](stories/US-004-tiered-execution-mvp.md) | story | Tiered execution MVP | — | ready | P1 |
| [T-007](tasks/T-007-tiered-m1-config-gate.md) | task | Tiered M1 — config + complexity gate | **A** | ready | P1 |
| [T-008](tasks/T-008-contextpack-schema.md) | task | Tiered M2 — ContextPack schema + reader | **B** | ready | P1 |
| [T-013](tasks/T-013-disk-watchdog-beat.md) | task | Beat 2.3 — disk/resource watchdog | **C** | ready | P1 |
| [T-014](tasks/T-014-memory-recall-beat.md) | task | Beat 2.4 — memory recall on repeat failure | **D** | ready | P2 |
| [T-001](../../S01-positioning-and-demo/tasks/T-001-github-about-topics.md) | task | GitHub About/topics (carry) | **E** | carry | P0 |

## Suggested order

```text
PR-A (T-007 M1 gate) → PR-B (T-008 M2 ContextPack)
PR-C (T-013 disk beat) + PR-D (T-014 memory beat)  # parallelizable anytime after start
PR-E (T-001) when gh auth works
```

Weekly 30–45m refinement (S01 retro action): groom tiered M3–M5 (`decision → execute → verify` DAG, escalation, cost harness) for **S04**, not this sprint.

## Explicitly out of scope

- Tiered M3–M5 (splitter, worker allowlists, escalation ladder, cost harness) — S04
- MCP board export / US-005 (Phase 3)
- SWE-bench / coding-agent UX
- Foundational baseline contract changes
- PRs that mix tiered runtime + beat docs + prod fixes

## Risks / dependencies

- M1 gate must be provably inert when off (below-threshold ≡ one-shot) — or M2+ builds on sand
- Disk beat needs fault injection without filling a real disk (tiny threshold on a scratch `--home`)
- Memory beat needs a seeded `{symptom, solution}` pair — depends on librarian/FTS fixtures, not live curation
- T-001 still gated on `gh` auth (carried since S01)

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at close (did M1 stay inert? did Phase 2 close? did PR budgets hold?).

## Links

- [PR-PLAN.md](PR-PLAN.md)
- [product-plan Phase 2 + Phase 5](../../../docs/product-plan.md)
- [tiered-execution spec](../../../docs/tiered-execution.md)
- [harness-reliability.md](../../../docs/harness-reliability.md)
- [S02 retro](../S02-harness-reliability/retro/RETRO.md)
