# T-012: Fix default Gemini model alias (PR-G)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P2 |
| Sprint | S02-harness-reliability |
| Parent | S01 retro action |
| Estimate | M |
| PR | **PR-G** — ≤15 files / &lt;400 LOC |
| Links | [S01 retro](../../S01-positioning-and-demo/retro/RETRO.md) |

## Goal

Remove stale `gemini-2.5-flash` default from main tree (config default + docs + tests that assert it). Prefer documenting LiteLLM; if a direct Gemini default remains, use a currently valid id **or** empty model with order-only cascade — decide in PR description.

## Allowed paths (main tree only)

- `internal/config/gateway.go`
- `config.reference.yaml`
- `docs/config-reference.md`
- `README.md` (example JSON only if still present)
- Tests currently hard-coding `gemini-2.5-flash` under `internal/**`, `cmd/agentd/**` (14 hits across 7 test files)

## Explicitly excluded

- `.kilo/worktrees/**`
- Unrelated gateway refactors

## Done when

- [ ] `grep -r gemini-2.5-flash` on main-tree paths above is empty (or only historical notes)
- [ ] Diff within budget
