# T-011: LiteLLM/Poolside-first run docs (PR-F)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Parent | US-003 (ops clarity) / S01 retro action |
| Estimate | S |
| PR | **PR-F** — ≤5 files / &lt;250 LOC |
| Links | [S01 retro](../../S01-positioning-and-demo/retro/RETRO.md), [demo.md](../../../../docs/demo.md) |

## Goal

README “First Run” leads with LiteLLM/Poolside (or mock), not stale Gemini-only as the happy path.

## Allowed paths

- `README.md`
- `docs/demo.md` (align wording only)
- `config.reference.yaml` (LiteLLM example comments only)
- `docs/openai-compatible-providers.md` or `docs/llm-connector-strategy.md` — link only if needed (count toward budget)

## Done when

- [ ] First-run section does not steer new users into dead `gemini-2.5-flash` as the primary path
- [ ] Points at LiteLLM aliases / Poolside
- [ ] Diff within budget
