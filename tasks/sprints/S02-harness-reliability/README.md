# Sprint S02 — Harness reliability pack

| Field | Value |
| --- | --- |
| Window | 2026-09-29 → 2026-10-13 |
| Goal | Prove the house — restart and fallback degrade to board state, not silent stuck RUNNING |
| Status | planned |

## Goal

Ship the first runnable harness reliability demos on top of the S01 demo path. Audience is an adopter evaluating unattended runs; success is "I trust board recovery" without chasing SWE-bench. Product plan Phase **2** (2.1–2.3).

## In scope

| ID | Type | Title | Status | Priority |
| --- | --- | --- | --- | --- |
| [US-003](../../backlog/stories/US-003-harness-reliability-pack.md) → `stories/US-003-harness-reliability-pack.md` | story | Harness reliability pack | ready | P0 |
| [T-006](tasks/T-006-harness-reliability-doc.md) | task | docs/harness-reliability.md + restart mid-task beat | ready | P0 |
| [T-010](tasks/T-010-provider-fallback-demo.md) | task | Provider fallback beat (cascade / breaker) | ready | P1 |
| [SP-004](spikes/SP-004-env-preflight.md) | spike | Env/tooling pre-flight (gh, make, Go, AGENTD_HOME) | ready | P1 |

## Spike order (what blocks what)

1. **SP-004** first hour — fixes the S01 retro gap: verify `gh`, `make`, Go, `AGENTD_HOME=/tmp/...` before any gh-dependent work; gates T-010 if auth needed.
2. **T-006** (restart mid-task, chosen in `../S01-positioning-and-demo/spikes/SP-001-reliability-beat.md`) — first demo beat.
3. **T-010** — second beat; reuse dead-`base_url` connector inject from `docs/demo.md` but assert cascade/breaker.
4. Weekly **refinement session** (30–45m) before S02 mid-point — groom `US-004`/`T-007`/`T-008` (tiered M1) + `US-005` (MCP) out of this sprint.

## Explicitly out of scope

- Tiered execution runtime (Phase 5 / M1+ — `US-004` / `T-007` / `T-008`)
- MCP board export (Phase 3 / `US-005`)
- SWE-bench / coding-agent UX chase
- Changing foundational baseline contract

## Risks / dependencies

- Restart beat needs kill + same `--home` resume without corrupting SQLite.
- Provider fallback needs a controllable cascade (`gateway.order`) and breaker tuning without real billable LLM calls — prefer mock/LiteLLM as in S01 demo.
- Disk/watchdog beat deferred to later in Phase 2 (not S02 unless T-006/T-010 ship early).

## Retro

Fill `retro/RETRO.md` at sprint end (include whether SP-004 + refinement ceremony stuck).

## Links

- Product plan Phase 2: `../../../docs/product-plan.md#phase-2--harness-reliability-story-not-swe-bench`
- Prior demo: `../../../docs/demo.md`, `../../../docs/harness-reliability.md` (stub)
- S01 retro: `../S01-positioning-and-demo/retro/RETRO.md`
