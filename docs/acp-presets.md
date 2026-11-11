# Running an external agent

How agentd runs opencode and cline as `acp` agents (S19 PR 08). What these agents did when measured is in [acp-agents.md](acp-agents.md).

Name an `acp` agent and route a stage to it ([config-reference.md](config-reference.md#routing-agents-and-stages)):

```yaml
agents:
  coder: { kind: acp, preset: opencode, model: "opencode/deepseek-v4.1-flash-free" }
stages:
  exec.turn:
    default: coder
```

Install the agent and sign in once, outside agentd (`opencode auth login`, or cline's own sign-in). agentd only reads that login: each run copies one provider's entry from it. The `model` is required, because a preset's default may be paid.

Each call:

1. finds the binary: `executable`, or the preset's command on `PATH`;
2. creates an owner-only `$TMPDIR/agentd-acp-*` holding the run's home and an egress proxy socket that allows only the preset's hosts (`sandbox.BashExecutor.StartEgressRun`);
3. writes the home and starts the agent in the jail, bound to the task's workspace. No bubblewrap, no run: the agent never gets the host's network;
4. removes the directory, login copy included, once the agent has exited.

| | opencode | cline |
| --- | --- | --- |
| Runs | `opencode acp` | the compiled `bin/.cline --acp --config $HOME/.cline`, not the Node wrapper |
| Egress | `opencode.ai:443` | `api.cline.bot:443`, `data.cline.bot:443` |
| Login copied | the model's provider (the part before `/`) from `auth.json` | `providers.json` cut to the `cline` provider, plus `cline-pass` (ClinePass) when the user has it; `lastUsedProvider` is `cline-pass` if present, else `cline`. The usage-billed `cline` login refuses even free models once its balance is negative. Other fields stay as written, or cline fails its schema and sees no login |
| Also in the home | `models.json` and `rg` from your `~/.cache/opencode`, so the catalog fetch (disabled) and the ripgrep download need no host | `cline_mcp_settings.json` (`CLINE_MCP_SETTINGS_PATH`) and `rules/agentd.md`, the SP-026 rules file |
| agentd's MCP server | http, from `session/new` | http, from the settings file. cline's ACP mode asks before every tool and ignores the file's `autoApprove`; the driver allows the tools of the server it attached (`agentd__*`, kind `other`) with no hook, and refuses any other `other` |
| Permissions | `OPENCODE_CONFIG_CONTENT` sets `bash` and `edit` to `ask` | cline asks on its own |
| Model | in `OPENCODE_CONFIG_CONTENT` | `CLINE_MODEL`: the session ignores the file's model and runs cline's paid default |
| Model check | `session/new` must report `model` as `configOptions.model`, or the call fails with `ErrModelMismatch` before any prompt. An unknown id is swapped silently (`opencode/deepseek-v4-flash-free` became `opencode/big-pickle`) | the same |
| Usage | its own counts | none: zero tokens, model `cline (no usage reported)` in `ai_calls` |
| Hub daemon | none | starts in the jail and dies with its PID namespace |

Limits:

- **Cline may refresh its token** into the run's copy, which is removed with the run. If cline rotates refresh tokens, your own `providers.json` would hold a used one. Not observed in SP-025.
- **Cold start is per call:** every call starts a new agent process and copies the login and seed files (about 11 MB for opencode).
- **The daemon's MCP socket** lives in `$TMPDIR/agentd-mcp-*` until the daemon shuts the ACP runtime down, and the cleanup removes the directory with it.
- **A run killed before it closes** (the daemon or the process killed) leaves its directory, login copy included, in `$TMPDIR` (owner-only).

## Live smoke (2026-11-05)

`TestLiveACPPresetsAgainstTheBuiltinAgent` (`-tags live`, `internal/queue/worker`): the SP-025 tasks through the worker's execution seam, the agent in its egress jail, each workspace checked on the host (`go test`, a `CountLines` behavior test, seed functions, files changed). One run per cell except cline/small, which recorded 2 runs (retries, if any, are not separated from first attempts), so a smoke result, not a rate.

| Agent | Task | State | `go test` | Behavior | Files changed |
| --- | --- | --- | --- | --- | --- |
| cline, `nvidia/nemotron-3-ultra-550b-a55b:free` | small | COMPLETED (2 runs) | pass | **fail**: `len(strings.Split(text, "\n"))`, so `""` is 1 line and a trailing newline adds one; its own test pins that | `main.go`, `main_test.go` |
| | multi-file | COMPLETED | pass | **fail** (same `CountLines`) | `count.go`, `count_test.go`, `main.go`, and a built `wc` binary |
| | ambiguous | BLOCKED: asked a human | n/a | n/a | none |
| opencode | all | not measured: the account's free tier was used up (`Free usage limit reached`) on `deepseek-v4.1-flash-free` and on `glm-5.3-flash-free` after one run of 17,552 tokens | | | |
| builtin agent, `codestral-latest`, default budgets (B-089 fixed) | small | BLOCKED: the pre-task elicitation asked what `CountLines("")` should return; no code written | pass (seed) | n/a | none |
| | multi-file | BLOCKED: asked `ask_human` which file holds `CountWords`, mid-change | **fail** | **fail** | `CountWords` lost; `count.go` without its `strings` import, `count_test.go`, `main.go` |
| | ambiguous | BLOCKED: asked a human | n/a | n/a | none |

The two seams differ before the agent runs: the builtin path runs the pre-task elicitation, which is skipped only for a description over 200 characters with success criteria or constraints, and an `acp` route skips it. The small task's description is 152 characters with no criteria, so only the builtin was asked to decide whether to clarify. Tasks from intake usually carry success criteria.

**Still open for SP-024:** opencode on the same tasks (after its free tier resets), tasks with success criteria so both seams start alike, and n ≥ 3 first-attempt runs per cell (retries excluded, reported separately).
