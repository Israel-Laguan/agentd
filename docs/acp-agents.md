# External agents over ACP: what we measured

agentd plans to drive external agent CLIs as an [ACP](https://agentclientprotocol.com/) client, with its own MCP server attached to each session (S17 SP-022, S18 T-042). This page records what two agents actually did when we tried it, so the driver is built on findings and not on what each agent advertises.

Measured 2026-10-21 with a throwaway client (not in the repo). Versions: opencode 1.19.0, cline 3.0.73. Each run uses a fresh temporary directory and one trivial task: read `hello.txt`, write the word it contains to `out.txt`. Three runs per agent is a smoke result, not a reliability rate.

## How each agent was started

| | opencode | cline |
| --- | --- | --- |
| Command | `opencode acp` | `cline --acp` |
| Protocol version | 1 | 1 |
| Credentials | `opencode auth login`, done outside ACP | a prior sign-in; the ACP `authenticate` methods are browser flows |
| Choosing a model | `session/set_config_option` with `configId: "model"` | `session/set_model` with `modelId` |
| Free models | ids ending in `-free`, e.g. `opencode/nemotron-3-ultra-free` | ids ending in `:free`, e.g. `nvidia/nemotron-3-ultra-550b-a55b:free` |
| Default model | `opencode/big-pickle` | `anthropic/claude-sonnet-5.5` (paid) |

**Pin the model in the session.** Cline's default is paid: with no credits, every prompt fails with `Insufficient balance` while the handshake still succeeds. Both agents list their models in the `session/new` response, so a preset can pick a free one per stage. The results below use Nemotron 3 Ultra on both agents.

## What each agent advertises

From `initialize` and `session/new`:

| Capability | opencode | cline |
| --- | --- | --- |
| MCP transports | http, sse (stdio is the protocol baseline) | none advertised |
| Prompt content | text, image, embedded resources | text, image; **no embedded resources** |
| Sessions | load, resume, fork, list, close | load |
| Modes | none | `plan`, `act` (default `act`) |

agentd's MCP server ([`mcp-board-export.md`](mcp-board-export.md)) serves both stdio and Streamable HTTP, so either agent can be given it. For cline, the context agentd builds has to be sent as plain text, because cline does not accept `resource` blocks.

## What each agent did

The client advertised `fs.readTextFile`, `fs.writeTextFile` and `terminal`, approved every permission request, and attached a one-tool MCP server whose only tool was `report_outcome` (`Completed | Failed | NeedsHuman | Split`, plus a summary). The prompt never named that tool; only its description asked for one call at the end.

| Measure | opencode | cline |
| --- | --- | --- |
| Task done | 3 of 3 | 3 of 3 |
| Wall time | 13-61 s | 7-20 s |
| `session/request_permission` | 0 | 2 per run |
| Client `fs/*` requests | 0 | 0 |
| Client `terminal/*` requests | 0 | 0 |
| `report_outcome` called | 3 of 3 (http, from `session/new`) | 0 of 3 from `session/new`; from cline's own MCP config, 1 of 3 unprompted and 3 of 3 with the tool named (see below) |
| Usage in the `session/prompt` result | input, output, thought and cached-read tokens | none, `stopReason` only |
| Tool detail in `session/update` | kind (`read`, `edit`, `execute`, `search`, `other`) and path or command in the title | kind and the raw arguments JSON in the title |
| Tokens for one trivial turn | about 21,000 | not reported |

## What this means for agentd

- **Neither agent sends file or shell I/O through agentd.** Both read, write and run commands themselves even when the client offers `fs` and `terminal`. agentd cannot deduplicate their file reads or jail their commands through ACP. A jail has to wrap the whole agent process (bubblewrap or podman, with network access to the model), and that is not tested yet. Tool counts and repeated reads for these agents can only come from their `tool_call` updates.
- **Only cline can be gated.** Cline asks permission before editing, so agentd's hooks and human approval can sit on that request. opencode, with its default settings, never asks.
- **opencode met the outcome contract with nothing extra.** It called `report_outcome` every time, from the server passed in `session/new`.
- **cline meets it only through its own config and a prompt that names the tool** (details in the next section).
- **Usage is self-reported or missing.** opencode reports its own token counts; cline reports none. Neither can be compared exactly with what agentd measures at its gateway ([`ai-usage-report.md`](ai-usage-report.md)), and a cline run would need another usage source.
- **An external agent has a fixed overhead.** About 21,000 tokens for a one-line task, mostly the agent's own system prompt and tool definitions. That is the floor for choosing an external agent over a single model call.
- **Attach only the tools a task needs.** In one opencode run with agentd's full board tools attached, the agent spent 43 s and about 68,000 tokens exploring the board, against 7-11 s and about 20,000 tokens with only `report_outcome`.

## Cline and MCP

Cline accepts `mcpServers` in `session/new` and then ignores it: the stdio server was never started (0 spawns in 6 sessions). Its ACP code never reads `params.mcpServers`. Cline only loads MCP servers from its settings file, `cline_mcp_settings.json`, whose path can be overridden with `CLINE_MCP_SETTINGS_PATH`.

**Cline runs a hub daemon that outlives the ACP process.** `cline --acp` starts `.cline --cline-hub-daemon` on a local port, and every later `cline --acp` connects to it, whatever its own `--config` or environment says. The daemon keeps the settings it started with, so a changed MCP config has no effect until it is stopped. A driver has to own that daemon: start it with its own `CLINE_MCP_SETTINGS_PATH`, and stop it when done, or other cline users on the machine inherit agentd's MCP server.

With a fresh hub daemon and the server in the settings file (stdio, `autoApprove: ["report_outcome"]`):

| Prompt | Server started | `report_outcome` called |
| --- | --- | --- |
| tool not named | 3 of 3 | 1 of 3 |
| "When you are done, call the report_outcome tool from the agentd MCP server." | 3 of 3 | 3 of 3 |

Cline names the tool `agentd__report_outcome` (server and tool joined by `__`); opencode names it `agentd_report_outcome`.

**Cline guesses when the tool is missing.** In the runs where it could not see the server, the named prompt made it search its other MCP tools (`composio_search_tools`, `composio_manage_connections`), search the workspace, and once run `npx -y @agentd/mcp report_outcome ...` through the shell. That package does not exist on npm, so the command failed, but the name could be registered by anyone. A driver must deny unknown `npx` and package installs through cline's permission requests, not approve them all as this test client did.

## Not measured yet

- Real agentd stages (intake, task creation, execution) compared across a single model call, agentd's builtin agent and each external agent.
- Running the agent process inside the jail.
- Whether a cline system prompt or rules file, rather than the task prompt, makes the outcome call reliable.
- agy, which has MCP but no ACP.

## Stage × method comparison

Same model everywhere: Nemotron 3 Ultra, free. Three runs per cell on a trivial Go project (a word counter, about 40 lines). **Builtin** is agentd's own agent: a local daemon pointed at OpenRouter, with `agentic_mode: true`. **L1** is one model call using agentd's own prompt. Workspace checks were run by hand afterwards (`go test`, expected functions and flags present, files changed or not), not taken from the agent's report. This is a smoke test, not statistics.

| Stage | Method | Contract success | Latency | Tokens per run | Tool calls | Workspace check |
| --- | --- | --- | --- | --- | --- | --- |
| Intake, clear request | builtin (`agentd ask`) | 3 of 3 plans, 5 tasks each | 17-35 s | about 2,200 (classify, scope, plan) | 0 | n/a |
| | opencode | 3 of 3 `submit_plan`, 5 tasks | 16-24 s | about 22,000 | 1 | n/a |
| | cline | 3 of 3 `submit_plan`, 5 tasks | 11-17 s | not reported | 1 | n/a |
| Intake, vague request | builtin | 1 of 3 became a plan (a discovery plan that tells the project to go and look at the workspace); 2 of 3 refused with "no plan drafted" | 8-33 s | 1,200-2,300 | 0 | n/a |
| | opencode | 3 of 3 `ask_human` | 6-22 s | about 21,700 | 1 | n/a |
| | cline | 3 of 3 `ask_human` | 5-8 s | not reported | 1 | n/a |
| Task creation, one phase | L1 | 3 of 3 valid task lists, 6 tasks each | 23-86 s | 2,000-2,300 | 0 | n/a |
| | opencode | 3 of 3 `submit_tasks`, 5-7 tasks, all with success criteria | 34-49 s | about 23,300 | 5 (a glob and 3 reads) | n/a |
| | cline | 3 of 3 `submit_tasks`, 5-6 tasks, all with success criteria | 25-28 s | not reported | 2-4 | n/a |
| Execution, small | builtin | 3 of 3 COMPLETED, **1 of 3 wrong** (a test it wrote fails: `CountLines("one\n")` returns 2) | 20-40 s | 8,900-9,200 | 6 | 2 of 3 pass `go test` |
| | opencode | 3 of 3 `report_outcome` | 30-41 s | about 23,200 | 7 | 3 of 3 pass |
| | cline | 3 of 3 `report_outcome` | 50-90 s | not reported | 9-14 | 3 of 3 pass |
| Execution, multi-file | builtin | 3 of 3 COMPLETED | 35-65 s | 14,500-23,000 | 10 | 3 of 3 pass, flags and both functions present |
| | opencode | 3 of 3 `report_outcome` | 64-76 s | 24,100-24,800 | 10-13 | 3 of 3 pass, flags and both functions present |
| | cline | 3 of 3 `report_outcome` | 43-161 s | not reported | 10-28 | 3 of 3 pass, flags and both functions present |
| Execution, ambiguous | builtin | 1 of 3 asked a human (a clarification task); **2 of 3 COMPLETED with nothing done** | 25-115 s | 0-19,900 | 1-2 | no change in 3 of 3 |
| | opencode | 3 of 3 `ask_human`; one run repeated the question and `report_outcome` twice | 11-39 s | about 22,000 | 1-4 | no change in 3 of 3 |
| | cline | 1 of 3 asked and stopped; 2 of 3 reported `Completed` after guessing a JSON format | 46-71 s | not reported | 9-14 | **`main.go` changed in 3 of 3** |
| Execution, any | L1 | not applicable: one model call has no workspace | | | | |

**Two caveats on the builtin execution rows.**

1. The first builtin runs used the default agent profile (`agentic_mode: false`), which runs `exec.legacy_command`. It marked every task COMPLETED, ran one `cat` and changed nothing, in 9 of 9 runs. Execution was only measured after setting `agentic_mode: true` on the agent.
2. The daemon's sandbox (bubblewrap, binding only `/usr`, `/lib` and `/bin`) cannot see the host's Go toolchain, which lives in `~/.local/go`. The first agentic run looped three times over reading files and `go test`, tried `apt-get install golang-go`, hit a sandbox path violation, and ended `FAILED_REQUIRES_HUMAN` after 900 s. The numbers above come from a second batch whose prompt said the toolchain was missing and that the owner would run `go test`. The external agents ran unsandboxed with the host's Go, so execution is not on equal terms.

What the numbers say:

- **L1 is about ten times cheaper where it applies.** Task creation is about 2,000 tokens as one model call and about 23,000 for opencode. The 21,000-token floor of an external agent is paid for every stage it handles, and buys nothing on a prompt that needs no workspace.
- **Intake: asking is the difference.** opencode and cline asked a question 6 of 6 times because they were handed an `ask_human` tool. agentd's pipeline refused twice and turned one vague request into a plan.
- **Execution: all three finish the clear tasks; they differ on being stuck.** On the ambiguous task opencode never changed a file and always asked. The builtin agent reported success on 2 of 3 without doing anything. cline edited the workspace in 3 of 3 and reported `Completed` in 2 of 3. A COMPLETED state is not evidence without a workspace check.
- **The builtin agent cannot verify what it writes when the toolchain is missing.** It wrote a failing test and reported success. That is a sandbox and policy gap in agentd, independent of any external agent.
- **Repeated reads are the builtin baseline.** In every small and multi-file run it read each file twice (4 reads of 2 files, 6 reads of 3), once to plan and once to verify. cline repeated one file in 4 of 6 runs. opencode's update titles carry no file path, so its repeats cannot be counted from the update stream.
- **Token reporting differs.** Builtin and opencode report tokens; cline does not. opencode's figure includes about 17,000 cached tokens of its own prompt.

Not measured: running an external agent inside the jail, agy, and the ambiguous task with the Go toolchain available to the builtin sandbox.

## Pitfalls when driving these agents

Errors hit while measuring them, with the fix for each:

- **Cline's default model is paid.** With no credits every prompt fails with `Insufficient balance`, and the handshake still succeeds. Pick a free model on the session first.
- **Cline ignores `mcpServers` in `session/new`.** Configure the server in `cline_mcp_settings.json` instead.
- **Cline's hub daemon outlives the ACP process** and keeps the settings it started with. Stop it before and after any settings test.
- **Killing a process in the same shell command can kill the shell.** Run the kill as its own command, with a bracketed pattern such as `pkill -f "cline-hub-daemo[n]"`.
- **Test servers must not use a port that a running agentd uses.** The dev daemon on 8765 once answered a probe's MCP calls and explored the real board.
- **A permission client that approves everything will approve `npx -y <unknown package>`.** Deny package installs and unknown commands.
