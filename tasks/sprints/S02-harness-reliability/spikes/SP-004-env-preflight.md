# SP-004: Env/tooling pre-flight (gh, make, Go, AGENTD_HOME)

| Field | Value |
| --- | --- |
| Type | spike |
| Status | done |
| Priority | P0 |
| Sprint | S02-harness-reliability |
| Time box | 1 hour |
| Links | [retro S01](../../S01-positioning-and-demo/retro/RETRO.md) |

## Question

Do we have the tooling to complete gh-gated and make-gated work without the S01 surprise?

## Run log (2026-09-14, acer)

| Check | Result |
| --- | --- |
| `gh --version` | **OK** — `~/.local/bin/gh` 2.67.0 |
| `gh auth status` | **FAIL** — not logged in (`gh auth login` still required for T-001 / PR-H) |
| `make` | **OK** — GNU Make 4.4.1 |
| `go version` | **OK** — go1.26.2 linux/amd64 |
| `make build` | **OK** — `bin/agentd` |
| `agentd --home /tmp/… init` | **OK** |
| `agentd start` with empty keys | **FAIL** — `no LLM providers available` |
| `agentd start` with dummy `OPENAI_API_KEY` + `base_url` set | **OK** — API listens (even if upstream is dead) |

## Output

- [x] Document install/auth gap for `gh`
- [x] Confirm make/Go/build/`AGENTD_HOME` init
- [x] Note start requires a configured provider (key or local OpenAI-compatible entry) — use LiteLLM/Poolside/mock or a dummy openai slot for daemon-only demos
- [x] Gate: **do not schedule PR-H (T-001) until `gh auth login`**; code/docs PRs can use git + manual PR or auth later

## Notes

For restart Beat 1 scripting, prefer a throwaway home with an openai-compatible entry so `start --skip-llm-warmup` comes up even before a healthy LLM is attached.
