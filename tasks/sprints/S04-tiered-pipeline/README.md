# Sprint S04 — Tiered pipeline (M3–M5)

| Field | Value |
| --- | --- |
| Window | Sprint window: 2026-09-19 – TBD (implementation complete) |
| Goal | Land tiered execution M3–M5: decision → execute → verify DAG with allowlists, escalation ladder + HUMAN handoff, cost/latency harness demo |
| Status | **Implementation complete — PR-C ready for review** |
| Based on | [S03 retro](../S03-tiered-foundation/retro/RETRO.md) |

## Goal

Complete the cost wedge (product-plan Phase 5, milestones M3–M5) on top of the S03 foundation (M1 gate + M2 ContextPack). Audience: operator evaluating tiered runs on hard tasks (M3/M4) + adopter comparing token/$ vs single-model baseline (M5).

**PR discipline:** see [`PR-PLAN.md`](PR-PLAN.md) — group tickets by allowed paths; measure with `git diff --stat main...HEAD` before opening a PR.

## In scope

| ID | Type | Title | PR | Status | Priority |
| --- | --- | --- | --- | --- | --- |
| [T-015](tasks/T-015-tiered-m3-splitter.md) | task | Tiered M3 — DAG splitter + step-kind profiles | **A** | done | P1 |
| [T-016](tasks/T-016-tiered-m3-modes.md) | task | Tiered M3 — Worker modes (context/decision/execute/verify) | **B** | done | P1 |
| [T-017](tasks/T-017-tiered-m4-escalation.md) | task | Tiered M4 — Escalation ladder + NEEDS_CONTEXT | **C** | done | P1 |
| [T-018](tasks/T-018-tiered-m5-cost-harness.md) | task | Tiered M5 — Cost/latency harness demo | **C** | done | P2 |
| [T-019](tasks/T-019-decision-propagation-question.md) | question | Decision artifact propagation — open design question | — | resolved | P2 |

## Suggested order

```text
PR-A (T-015 DAG splitter) → PR-B (T-016 worker modes)
PR-C (T-017 escalation + T-018 cost harness, merged) after PR-B
```

**PR-C note (operator decision 2026-09-19):** T-017 and T-018 were originally separate PRs (see [`PR-PLAN.md`](PR-PLAN.md)) and are now merged into one, in order to clear PR-C's 20-file minimum floor. This supersedes the previous "PRs that mix runtime changes + cost harness + escalation" rule (removed from "Explicitly out of scope") for PR-C specifically.

## Explicitly out of scope

- Splitting every task (complexity gate stays the gate — M1 is done)
- Replacing Frontdesk project planning
- SWE-bench / coding-agent UX
- MCP board export / US-005 (Phase 3)
- Foundational baseline contract changes

## Risks / dependencies

- M3 splitter must produce real DAG children (SPAWNED_BY + DEPENDS_ON), not one chat pretending to be four roles
- Worker modes need tool allowlists per step kind — broad search forbidden outside pack paths
- Escalation ladder must degrade to HUMAN without a stuck loop (S02 reliability story)
- Cost harness needs a fixed task pack for reproducible measurement — cannot use random tasks

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at close.

## Links

- [PR-PLAN.md](PR-PLAN.md)
- [product-plan Phase 5](../../../docs/product-plan.md)
- [tiered-execution spec](../../../docs/tiered-execution.md)
- [S03 retro](../S03-tiered-foundation/retro/RETRO.md)
