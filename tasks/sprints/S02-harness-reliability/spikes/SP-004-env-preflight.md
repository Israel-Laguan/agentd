# SP-004: Env/tooling pre-flight (gh, make, Go, AGENTD_HOME)

| Field | Value |
| --- | --- |
| Type | spike |
| Status | ready |
| Priority | P1 |
| Sprint | S02-harness-reliability |
| Time box | 1 hour |
| Links | [retro S01](../../S01-positioning-and-demo/retro/RETRO.md), [T-001](../../S01-positioning-and-demo/tasks/T-001-github-about-topics.md) |

## Question

Do we have the tooling to complete gh-gated and make-gated work without the S01 surprise ("failing to see gh was not installed was meant to be a spike")? Should also cover the recurring refinement session need noted in retro.

## Method

1. Check `gh auth status`, `gh --version`; if missing, document install (`gh auth login` vs `GH_TOKEN`) and note whether to carry gh-gated tasks.
2. Check `make --version`, `go version`, and an `AGENTD_HOME=/tmp/agentd-sp004` init smoke (`make build && ./bin/agentd --home /tmp/agentd-sp004 init`).
3. Run one board smoke: `agentd start --skip-llm-warmup` + `curl /api/v1/system/status`.
4. Document pass/fail per tool and whether S02 T-010 needs mock-only path.

## Output

- [ ] Checklist with versions + gh auth state
- [ ] Note if gh-gated asks should be mock-only this sprint
- [ ] Add weekly refinement session to S02 cadence (30–45m, groom US-004/005 + T-007/T-008) — retro action

## Out of scope

Fixing provider/model staleness (separate S02 README task); full harness beats.

## Notes

Time-box hard; cap at 1h. If gh remains unavailable, mark downstream tasks mock-only and carry gh push.
