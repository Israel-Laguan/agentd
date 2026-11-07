# Moving the builtin harness out of agentd

agentd is a coordinator, not a harness. This page records that decision, what stays in agentd, what leaves, and the order in which it leaves. It replaces the kit plan in [agent-kit.md](agent-kit.md).

**Decision (owner, 2026-11-05):** external agents only in production builds; the devenv/e2e build may include a tagged fake ACP agent for tests (a test-only scripted agent that speaks ACP, not the builtin tool loop). The builtin tool loop and everything only it uses move to a `_harness/` folder, and in the last step to a repository of their own. That repository is "a harness written with ACP and MCP support, among other features, that was planned to work with agentd". Its tasks and improvements are not tracked in agentd, and it gets no maintenance. agentd drives ready-made agents (opencode, cline, or any agent that speaks ACP and MCP) as black boxes.

## What agentd is

1. `agentd start` loads config and env, opens the database, checks providers and serves the API.
2. The queue loop runs crons, claims tasks from the board, and hands the ones that need work to an agent.
3. The chat front door (intake) turns a request into a plan and tasks.
4. Outcomes come back, are checked and logged (`ai_calls`, task events, audit), and the core moves the task: done, blocked on a human, failed, retried or split.

Two kinds of AI serve that loop:

| Kind | Level | Used for | Today |
| --- | --- | --- | --- |
| Direct model call | L1 (`kind: model`) | Chat and intake, clarify, split and replan, memory, summaries, quick reviews and decisions | 12 of the 22 purposes; the gateway |
| External agent | L3 (`kind: acp`) | Kanban tasks that need a workspace (`exec.turn`) | opencode and cline presets over ACP, in the jail, with agentd's MCP contract tools |

The builtin loop issues 8 purposes of its own (work plan, respec, repair section, permission alternative, topic drift, compact context, subagent turn, embed files); 5 of them are issued from `queue/worker`. They leave with it.

**A black box is not trusted.** SP-026 measured opencode passing the behavior check on 5 of 10 small tasks, both agents reporting Completed on 10 of 10 impossible tasks by faking the dependency, and cline reporting Completed on 6 of 10 ambiguous tasks without a rules file ([acp-agents.md](acp-agents.md#reliability-at-n--10-sp-026)). Agents propose through MCP (outcome, question, comment, memory); the coordinator checks the workspace and writes the board.

## What the harness repository is, and is not

It holds the builtin tool loop, its tools (bash, read, write, with the B-089 write refusal), in-loop context management, planning, subagents, checkpoints and rewind, file context, the capability registry, and the plugin loader and MCP capability adapter that were never wired in (B-063). It also takes the tiered pipeline and the worker code that hosted the loop.

- **It does not speak ACP as an agent.** agentd's ACP code (`internal/agent/acp`) is the client that drives opencode and cline. It stays in agentd.
- **Its MCP support is a client adapter** (`capabilities/mcp`) that nothing imports.
- **It does not build on its own.** agentd's worker drove it in-process through `agentic.Host` (28 methods), and it imports agentd's `models`, `config`, `gateway`, `sandbox`, `agent/hooks` and `stage`. Turning it into a standalone ACP agent is new work, done in that repository if anyone needs it.
- **Its backlog starts in `_harness/backlog/`:** the bugs and tasks agentd planned for the harness and never ran, marked "planned in agentd, never executed". agentd adds harness work there, not to its own backlog.

## The boundary

| Stays in agentd | Leaves (to `_harness/`) |
| --- | --- |
| `queue`, `queue/worker` (minus the host files), `queue/{planning,hostreport,recovery,safety}` | `queue/worker/agentic` |
| `stage`, `stage/intake`, `frontdesk`, `memory`, `kanban/*`, `api/*`, `bus`, `services`, `aicalls`, `cmd/agentd` | `agent/context`, `agent/filecontext`, `agent/subagent` |
| `gateway` and its subpackages: the L1 path | `agent/runtime` runner, loop result, ports and topic guard |
| `agent/acp` and `fakeagent`, `mcp`, `agent/hooks`, `sandbox`, `sandbox/egress` | `agent/tools`, minus what the core uses |
| `agent/skills`: skill text is context, given to every agent (T-067) | `agent/session` checkpoints |
| From `agent/runtime`: the budget guard and the task audit | `capabilities`, `capabilities/plugin`, `capabilities/mcp` |
| From `agent/tools`: `ask_human`, tool names, `ToolResult`, `ToolStatus`, `BuildSandboxEnv` | Worker host files: `worker_agentic*`, `worker_tools*`, `worker_permission`, `targeted_redo`, `phase_splitter`, `worker_plugins` |
| From `agent/session`: the secret store | The tiered pipeline: `worker_tiered*`, `escalation*` |
| `config`, `models`, `paths`, `toolenv`, `markdown` | Docs: `agentic-harness.md`, `agentic-harness-roadmap.md`, `harness-reliability.md`, `tiered-execution.md`; journeys J12, J19, J20, J21 |

Measured 2026-11-05 (`main` at `439236b9`): the leaving code is 164 files and 24,554 lines with tests (10,742 non-test lines, 18.5% of the 58,018 in `internal/` and `cmd/`), plus 16 files and 3,862 lines of the tiered pipeline.

## Why a `_harness/` folder

The go tool skips directories whose names start with `_`, so `go build ./...`, `go test ./...`, `go vet` and golangci-lint never load it: it costs agentd no maintenance while it waits. `tools/checkloc` and `tools/checkminfunc` read `git ls-files` and need `_harness/**` in their exclude lists. Moving files there is a rename, which the size gate counts as files, not lines.

The move can only happen once nothing in agentd imports the harness, so the coordinator features that today run only on the builtin path move to the seam first.

## Order

Sizes are estimates. Every move re-keys the `minfunc` baseline and `purpose_sites_test.go`.

| Step | Ticket | What | Done when |
| --- | --- | --- | --- |
| 1 | T-066 | depguard rule `core-no-harness`: core packages, tests included, may not import harness packages. Today's violators are listed as exceptions that later PRs remove | `make lint` |
| 2 | T-066 | Coordinator pieces leave harness packages: budget guard, task audit, `ask_human`, tool names and result types, secret store, scheduler task types, `BuildSandboxEnv`; the loop's import of `acp.ErrSuspended` is inverted. Alias facades at the old paths | The rule has no exceptions for `mcp`, `agent/hooks`, `agent/acp`, `queue`, `config`, `cmd` |
| 3 | B-090 | A batch fallback slot runs on the task's routed agent, not the loop | Red-then-green Go test with the fake ACP agent |
| 4 | T-067 | An ACP agent gets the context the builtin gets: AGENTS.md, user preferences, memory lessons and preferences, matched skills, review feedback, the previous attempt's failure | Fake-agent test reads them in its prompt; the builtin's golden prompt is unchanged |
| 5 | T-068 | The ACP path runs the pre-task clarify, the success-criteria (B-039) and breakdown (T-055b) checks, and review-required tasks | Fake-agent tests for each |
| 6 | T-069 | The jail in devenv: `bubblewrap` in the images, `security_opt: [unmask=ALL]` on the agentd services, the fake ACP agent built into the e2e image; then the execution journeys move to it | A journey runs a task on the fake agent in the jail, in `make test-e2e` and `ci-e2e` |
| 7 | T-070 | Cut the wiring: dispatch has no builtin branch, a task that reaches `exec.turn` with no route fails `FAILED_REQUIRES_HUMAN` with a clear message, startup warns when no `acp` agent is routed, the 15 coordinator test files that drive the loop move to the fake agent. The harness's config keys fail at load as removed keys | `core-no-harness` has no exceptions outside the host files; removed-key tests |
| 8 | T-071 | `git mv` the leaving code, docs and journeys into `_harness/`, with a README that states its status | `make check` green; no `go list ./...` package under `_harness` |
| 9 | T-072 | Publish the harness repository from `_harness/` history, then remove the folder from agentd | `_harness/` gone; README links the repository |

Steps 1–6 are S20 (PRs 10–16 in its plan), 7–8 are S21, 9 is S22. The feature after it (US-011, below) is planned for S23.

## After the harness leaves

External agents are then agentd's only executors, and each has quirks its ACP support does not cover: where its binary and login live, how it takes a model, whether it asks permission, where it reads agentd's MCP server from, whether it needs a rules file, whether it reports usage. For opencode and cline those are Go code today (`internal/agent/acp/presets.go`). US-011 (S23) defines the next feature: an adapter per agent, declared as data and loaded from agentd's own config, so a new agent needs no code change and gets no way around the coordinator's checks. It is built when a third agent is wanted.

## What agentd loses

No ACP equivalent exists for these; they leave with the harness:

- healing's model tuning (`increase_effort`, `upgrade_model`); retry, split and the human handoff stay;
- credential injection into the shell (`agentic.tool_credentials`);
- one `ai_calls` row per turn: an ACP call records one row, with the agent's own token count (cline reports none);
- the B-043 refusal of the last attempt's failing command; the handoff note (T-051) carries the failure instead;
- topic guard, rewind, subagents, file context, tool manifests, in-loop planning;
- execution with no install: a task now needs opencode or cline, and the jail. On a host without a usable bubblewrap, ACP is refused (`sandbox/agent_command.go`).

## Open decisions

- **The final removal and the size gate.** Deleting `_harness/` is about 28,000 lines in about 186 files. Within the PR limit that is about 15 PRs; as 2 PRs (the file limit) it needs `large-pr-ok` after the gate fails. Owner call.
- **The tiered pipeline leaves with the harness** (default). It escalates from a small model to a large one inside the builtin; on ACP every step would be a separate agent call, at about 21,000 tokens each.
- **Text-only tasks and the chat.** `exec.turn` needs L3, so a question the chat turns into a text task would cost an agent call (about 21,000 tokens) instead of one L1 call (about 2,000). Spike SP-029 (S20) decides what the chat needs: direct calls with data agentd gathers, a read-only external agent, or both by intent. A tool loop inside agentd is ruled out: it would rebuild the harness.
- **The harness repository** needs a name (default `agentd-harness`), keeps Apache-2.0 with a NOTICE, and is created and pushed by the owner.

## The jail in rootless podman

Probe on 2026-11-05, podman 5.4.2, the existing `localhost/agentd:local` image, no network, nothing downloaded:

```sh
podman run --rm --network none --entrypoint sh localhost/agentd:local -c \
  'unshare -U -r true; unshare -U -r -m sh -c "mount -t tmpfs none /tmp"; unshare -U -r -n true; unshare -U -r -p -f --mount-proc true'
```

User, mount and network namespaces work inside the container. Mounting `/proc` in a new PID namespace fails (`Operation not permitted`) and works with `--security-opt unmask=ALL`. The jail uses `--unshare-all` and `--proc /proc` (`sandbox/bubblewrap.go`), so it needs that option. The local rootless-Podman J24 run now verifies bubblewrap in the dev image and podman-compose passing the option (T-069); GitHub-runner compatibility remains unverified (Go CI already needs `tools/ci/allow_bwrap_userns.sh` for AppArmor).
