# Sprint S02 — Harness reliability pack

| Field | Value |
| --- | --- |
| Window | 2026-09-29 → 2026-10-13 |
| Goal | Prove the house — restart and fallback degrade to board state, not silent stuck RUNNING; PRs stay ≤25 files / &lt;1k LOC |
| Status | active |
| Based on | [S01 retro](../S01-positioning-and-demo/retro/RETRO.md) |

## Goal

Ship the first runnable harness reliability demos on top of the S01 demo path. Audience: adopter evaluating unattended runs. Product plan Phase **2** (2.1–2.3).

**PR discipline:** see [`PR-PLAN.md`](PR-PLAN.md) — group tickets by allowed paths; measure with `git diff --stat main...HEAD` before opening a PR.

## In scope

| ID | Type | Title | PR | Status | Priority |
| --- | --- | --- | --- | --- | --- |
| [US-003](stories/US-003-harness-reliability-pack.md) | story | Harness reliability pack | — | ready | P0 |
| [SP-004](spikes/SP-004-env-preflight.md) | spike | Env/tooling pre-flight | none | done | P0 |
| [T-006a](tasks/T-006a-restart-beat-docs.md) | task | Restart beat docs/script | **A** | done | P0 |
| [T-006b](tasks/T-006b-restart-reconcile-tests.md) | task | Restart reconcile tests | **B** | ready | P0 |
| [T-006c](tasks/T-006c-restart-reconcile-fix.md) | task | Restart prod fix (if needed) | **C** | backlog | P0 |
| [T-010a](tasks/T-010a-fallback-beat-docs.md) | task | Fallback beat docs/script | **D** | ready | P1 |
| [T-010b](tasks/T-010b-cascade-breaker-tests.md) | task | Cascade/breaker tests | **E** | ready | P1 |
| [T-011](tasks/T-011-litellm-first-run-docs.md) | task | LiteLLM-first README | **F** | ready | P1 |
| [T-012](tasks/T-012-gemini-default-alias.md) | task | Drop stale gemini-2.5 default | **G** | ready | P2 |
| [T-001](../S01-positioning-and-demo/tasks/T-001-github-about-topics.md) | task | GitHub About/topics (carry) | **H** | carry | P0 |

Wrappers (tracking only): [T-006](tasks/T-006-harness-reliability-doc.md), [T-010](tasks/T-010-provider-fallback-demo.md).

## Suggested order

```text
SP-004 (1h)
  → PR-A (T-006a) → PR-B (T-006b) → PR-C only if needed
  → PR-D (T-010a) → PR-E (T-010b)
  → PR-F (T-011) → PR-G (T-012)   # can parallelize after SP-004 with A/D
  → PR-H (T-001) when gh auth works
```

Weekly 30–45m refinement (S01 retro action): groom tiered `US-004`/`T-007`/`T-008` for **S03**, not this sprint.

## Explicitly out of scope

- Tiered execution runtime (Phase 5 / M1+)
- MCP board export (Phase 3)
- Disk/watchdog / memory-recall beats (later Phase 2)
- SWE-bench / coding-agent UX
- Foundational baseline contract changes
- PRs that mix docs + large test + prod fix packages

## Risks / dependencies

- Restart beat: unclean kill + same `--home` must not corrupt SQLite
- Fallback: controllable cascade via mock/LiteLLM (no billable dependency)
- Gemini default rename can fan out tests — stay inside PR-G allowlist (~11 files)

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at close (did PR budgets hold? did SP-004 stick?).

## Links

- [PR-PLAN.md](PR-PLAN.md)
- [product-plan Phase 2](../../../docs/product-plan.md)
- [demo.md](../../../docs/demo.md), [harness-reliability.md](../../../docs/harness-reliability.md)
- [S01 retro](../S01-positioning-and-demo/retro/RETRO.md)
