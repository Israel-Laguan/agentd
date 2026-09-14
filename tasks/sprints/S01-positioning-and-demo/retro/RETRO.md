# Retro: S01 — Positioning and demo path

| Field | Value |
| --- | --- |
| Sprint | S01-positioning-and-demo |
| Date | 2026-09-14 |

## Went well
- Positioning artifacts shipped without runtime churn: hero (`README.md:1`), `why-agentd` (`docs/why-agentd.md:1`), demo path (`docs/demo.md:1`) all landed on `docs/s01-positioning` (3 commits).
- SP-003 `spikes/SP-003-demo-path-dry-run.md:104` gave a **GO** with a deterministic HUMAN beat (dead `base_url → 127.0.0.1:1 → ErrLLMUnreachable → BLOCKED + Manual review: AI providers unavailable`) — avoids sudo/permission roulette.
- SP-001/SP-002 closed open questions: restart mid-task = S02 Beat 1 (`spikes/SP-001-reliability-beat.md:18`); tiered defaults (threshold 200, file ContextPack, verify owns decision-specified checks) written to `docs/tiered-execution.md:1`.

## Went poorly
- T-001 blocked to end of sprint on repo-settings auth — assumed `gh` was available, but session showed `gh auth login` missing (`gh repo view` stderr). No pre-flight env spike.
- Gemini default model stale (`gemini-2.5-flash` → 404) forced SP-003 onto local mock `127.0.0.1:18080`; docs now warn but README first-run still mentions `gemini-2.5-flash`.

## Surprises
- Workspace empty → 409 until `POST /api/v1/projects/{id}/workspace/ready` (`spikes/SP-003-demo-path-dry-run.md:110`).
- Breaker `OPEN` after exactly 3 `ErrLLMUnreachable`; `LLM_OUTAGE_HANDOFF` System Offline not observed within ~25s even with `handoff_after: 10s`.
- Mock must speak full JSON modes (intent/scope/plan/worker); naive mock triggers healing ladder (`Manual review: self-healing failed`) instead of provider-exhausted path.

## Actions (assign + due)

| Action | Owner | Due |
| --- | --- | --- |
| T-001 GitHub About/topics applied + screenshot in PR (`Local daemon that turns an approved plan into a durable Kanban board ... SQLite source of truth.` + 6 topics) | repo admin (operator) | 2026-09-15 |
| New spike SP-004: env/tooling pre-flight (verify `gh`, `make`, Go, `AGENTD_HOME`) — 1h time-box | assignee TBD | S02 planning |
| Weekly backlog refinement session (30–45m) — groom US-003/004/005 + T-006..T-008, confirm estimates | facilitator TBD | before S02 start |
| Pin working Gemini/LiteLLM model alias in README + `config.reference.yaml` (remove stale 2.5 default) | docs owner | S02 |
| Optional `make demo-smoke` offline mock path for CI (product-plan Phase 1.4) | platform | backlog |

## Carry into next sprint
- **S02 = harness reliability pack** (`US-003` / `T-006` restart beat from `SP-001`). Connector HUMAN stays in `docs/demo.md:1` — do not re-prove. No tiered runtime, no MCP export, no SWE-bench (same out-of-scope as S01).
- If T-001 GH apply not merged before PR, carry explicitly with owner/due.
- Process carry: add `SP-004` env pre-flight and recurring refinement ceremony (covers "failing to see gh not installed was meant to be a spike" gap).
