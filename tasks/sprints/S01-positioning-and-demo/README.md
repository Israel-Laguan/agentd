# Sprint S01 — Positioning and demo path

| Field | Value |
| --- | --- |
| Window | 2026-09-14 → 2026-09-28 |
| Goal | Make agentd’s control-plane slot obvious, and ship a documented 10-minute approve→board→HUMAN demo path |
| Status | active |

## Goal

Cold readers get the one-liner in under 30 seconds; an operator can run the governance demo without tribal knowledge. Product plan Phases **0** and **1**.

## In scope

| ID | Type | Title | Status | Priority |
| --- | --- | --- | --- | --- |
| [US-001](stories/US-001-clear-positioning.md) | story | Clear public positioning | ready | P0 |
| [US-002](stories/US-002-ten-minute-demo.md) | story | 10-minute governance demo | ready | P0 |
| [T-001](tasks/T-001-github-about-topics.md) | task | GitHub About + topics | ready | P0 |
| [T-002](tasks/T-002-readme-hero.md) | task | README hero + what/not | done | P0 |
| [T-003](tasks/T-003-why-agentd.md) | task | docs/why-agentd.md contrast | done | P0 |
| [T-004](tasks/T-004-demo-doc.md) | task | docs/demo.md 10-minute path | done | P0 |
| [T-005](tasks/T-005-link-demo-from-readme.md) | task | Link demo from README Quickstart | done | P1 |
| [SP-003](spikes/SP-003-demo-path-dry-run.md) | spike | Demo path dry-run (blocks T-004) | done | P0 |
| [SP-001](spikes/SP-001-reliability-beat.md) | spike | Pick first harness reliability beat | ready | P1 |
| [SP-002](spikes/SP-002-tiered-open-questions.md) | spike | Lock tiered-execution open questions | ready | P2 |

## Spike order (what blocks what)

1. **Nothing** blocks T-001–T-003 (GitHub/README/why-agentd) — start those anytime.
2. **SP-003** before T-004/T-005 — ask→approve→board, then **inject connector failure** (dead `base_url` / kill upstream) to force HUMAN handoff.
3. **SP-001** after demo doc shape is clear — chooses S02 reliability beat; does not block Phase 0.
4. **SP-002** anytime this sprint, but **required before** backlog US-004 / T-007 (tiered M1). Not required to start S01 positioning.

## Explicitly out of scope

- Implementing tiered execution runtime (Phase 5 / M1+)
- MCP board export (Phase 3)
- SWE-bench / coding-agent UX chase
- Changing foundational baseline contract

## Risks / dependencies

- GitHub About/topics need repo settings access
- Demo path may need a mock/offline LLM path for CI-friendly smoke

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at sprint end.
