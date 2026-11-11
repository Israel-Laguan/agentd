# What the chat needs: direct calls, a read-only agent, or both (SP-029)

agentd's chat (`POST /v1/chat/completions`, `frontdesk.Planner`) and comment intake (`frontdesk.IntakeProcessor`) answer no question themselves. They report board status, plan, or ask back, and every question becomes a text task that the builtin loop runs. That loop leaves agentd ([harness-extraction.md](harness-extraction.md)), so this page records what the chat needs, what a direct model call can do with data agentd gathers itself, and what a read-only external agent costs. No tool loop inside agentd is considered under any option.

Measured 2026-11-11 on `main` at `f18b820d`. Claims are tagged **[run]** (measured here, with the run count), **[read]** (read from the code) or **[inferred]**.

## Summary

- **Recommendation: (c), both, chosen by intent.** The chat answers board questions and general questions itself, with one direct call or none. A question that needs the workspace becomes a read-only agent task on the existing `exec.turn` path. **Do not add the intake purposes to `stageAgentPurposes`:** that change alone wires nothing (below), and a read-only agent answer is too slow and too costly to sit inside a chat turn.
- **A read-only agent answers workspace questions correctly; a direct call cannot.** opencode answered 8 of 9 correctly [run, 9 runs]. A direct call given AGENTS.md, README and the file list said "not in the gathered files" on a question that needs a search [run, 1 answered run].
- **The agent costs 14–83 times the tokens of a direct call** and tens of seconds. opencode used 46,285–274,864 tokens per answer (p50 about 103,000) and 17–127 s (p50 44 s), against about 3,300 tokens and about 1 s for one direct call [run: 9 opencode runs on `opencode/deepseek-v4.1-flash-free`, 3 answered `codestral-latest` runs]. The 21,000-token floor (SP-022, opencode 1.19.0, 3 runs, 2026-10-21) is the cost of a trivial task. A real read-only question cost 2 to 13 times that floor [run, same 9 runs].
- **cline is slow, often empty, and not viable for chat** [run, 36 runs over 4 batches, cline 3.0.74]. Batch 1: `nvidia/nemotron-3-ultra-550b-a55b:free` on the preset's usage-billed `cline` login, 9 of 9 `Insufficient balance` (credits $-0.44). Batch 2: `cline-free/solar-mini4`, 9 of 9 stopped at `session/new` with `ErrModelMismatch` (cline ran its paid default), because ACP sessions take the provider from `CLINE_PROVIDER`, not `lastUsedProvider` [read, cline binary]. Batch 3, after the preset fix below: solar-mini4 on ClinePass ran, read and searched the workspace, and tried `report_outcome` 53 times; the driver refused every call, so 9 of 9 ended with no outcome. Cause: cline titles the call `agentd__report_outcome: {args}` and names the board tools `agentd__board_list_projects_<hash>`, while `ownTool` (`agent/acp/permission.go:248`) wants the exact title `agentd__<tool>` [read, run]. Before the fix, every cline route in agentd failed that way [inferred]. Batch 4, after both fixes (`ownTool` now matches the name before the colon and the hash only on the dotted board tools): of 9 runs, 3 hit the ClinePass inference cap (`INFERENCE_CAP_ERROR`, counted as quota errors, not wrong answers); of the other 6, **2 right** (Q1, Q2), **2 reported `{"answer": ""}`** and the contract accepted it (B-100), 1 ran into the 8-minute timeout with no outcome, 1 ended its turn cancelled with no outcome and no refusal. Wall time over those 6: 1 min 58 s to 8 min (two hit the timeout), p50 about 5 min. First update after 4.2–6.6 s. Tokens not reported. 0 permissions refused, 0 write attempts.

## Corrections to the ticket's facts (2026-11-05, `439236b9`)

1. **The seam does not carry the chat.** `Planner.Agent` is a `stage.Agent`, but it is wired to `stage.NewModelAgent(deps.Gateway)` (`api/server/server.go:60`). Comment intake is wired the same way (`cmd/agentd/start_runtime.go:49`) [read]. Only the worker routes a stage to an acp agent (`routedStageAgent`, `callACP` in `queue/worker/acp_routing.go`), and that path needs a task, a project with a `WorkspacePath`, and an agent profile [read]. Adding purposes to `stageAgentPurposes` would only let the config load. Unscoped chat has no project, and so no workspace to bind [read].
2. **`AccessNone` binds the workspace read-only.** `callACP` passes `readOnly = req.Access != AccessWrite` to the jail. `NoWorkspace` only refuses permission requests [read], and opencode does not ask before a read (preset config `{"bash": "ask", "edit": "ask"}`). So under `AccessNone`, opencode can still read the workspace [inferred].
3. **A question becomes a text task only on some paths.** Comment intake and approved-scope chat go straight to `intake.Plan` [read]. Unscoped chat classifies first, and the classifier has no "question" intent. On `codestral-latest` it sent explain and pros/cons questions and "Where is the per-phase task cap configured?" to `plan_request` in 7 of 7 answered runs, and "What does this project do?" to `ambiguous` in 1 of 1 [run, 8 answered of 20; the other 12 were 429s].
4. **A text task gets write access on the ACP path.** `runACPExecution` takes its access level from `profile.WorkspaceReadonly`, not from `requires_workspace_change` (`queue/worker/acp_execution.go:52-59`) [read]. After T-070 a question would run on an agent that can write, unless its profile is read-only.
5. **Every ACP stage call cold-starts the agent** (`acp_routing.go:108`, marked as a shortcut) [read].
6. **A direct call's gathered data is capped at 12,000 chars per message.** The router's default (`routing/router.go:19`) and `gateway.truncator.max_input_chars` (default 12,000) cut each message head and tail [read]. A 35,432-char gathered prompt reached the model as about 3,300 tokens [run, 3 answered `codestral-latest` runs].

## Capability table

| Capability | Needed | Evidence | Served by | Direct call: what agentd gathers | Tools or workspace | Tokens, latency |
|---|---|---|---|---|---|---|
| Board status, "what is blocked and why" | must | J03, `status_*_test.go`, `openai_intake.feature` [read] | `StatusSummarizer`, no model call. Attention items carry `required_action` and `explanation` [read] | Store reads (exist) | no | 0 tokens [read] |
| Plan and clarify | must | J04, J13, comment intake, B-085 tests [read] | Direct calls: classify, scope, plan | Intent, attachments, memory, house rules (exist). AGENTS.md and README for brownfield work: nice | no | About 2,200 tokens, 17–35 s (SP-022, Nemotron 3 Ultra free, 3 runs, 2026-10-21) |
| OpenAI wire shape | must | J13 [read] | `chat_wire.go` | n/a | no | n/a |
| A general question | must on comment and scoped intake; unscoped chat sends it to `plan_request` (fact 3) | Plan-prompt rule, live flag tests [read] | Today: a text task | One call with the question | no | Not measured: 3 of 3 runs were 429s. An answer-sized call is estimated at under 1,000 tokens, about 1 s [inferred from the classify calls: 0.62–0.84 s, 8 answered `codestral-latest` runs] |
| A question about the project's code | must | "What does this project do?" in the live flag set [read] | Today: a text task | AGENTS.md, README, file list. That answers "what does it do" [run, `codestral-latest`, 2 of 2 right] but not a search question [run, `codestral-latest`, 1 of 1 said "not in the gathered files"] | **Workspace read** for search questions | Direct call: about 3,300 tokens, 0.75–1.01 s [run, `codestral-latest`, 3 answered runs]. opencode: table below |
| Host facts | must | `openai_intake.feature`, plan prompt, `hostreport` [read] | A plan of command tasks; `hostreport` renders their output files with no model [read] | Not a chat answer: commands must run | **Exec and writes**: stays an `exec.turn` task | Unchanged |
| Explain a failure | nice | No chat path or test [read] | Status explanation, handoff note (T-051) | Events, `ai_calls`, comments, handoff note | no [inferred] | One call [inferred] |
| Memory, "what did we decide" | nice | Recall feeds planning only (`prependRecalledContext`) [read] | — | `Retriever.Recall` | no | One call [inferred] |
| Act on the board (retry, split) | no | No chat path or test; REST has `/retry`, `/split`, `/assign` [read] | REST | — | no | — |

## opencode, read-only, through the seam

Setup [run]: opencode `opencode/deepseek-v4.1-flash-free`, driven by `Worker.acpDriver` in the egress jail with the workspace bound read-only (`AccessReadOnly`), and `ACPAgent`'s session flow with an `{answer}` contract. The workspace was a `git archive` of `HEAD`. The task hooks and `ACPPermitter` answered permissions. 3 runs per question, 8 s apart. Correctness was checked by hand against the repository.

| Question | Right | Tokens (agent-reported) | Wall time | Bash permission requests | Write attempts |
|---|---|---|---|---|---|
| What does this project do? | 3 of 3 | 46,285 / 55,510 / 74,240 | 57 / 44 / 20 s | 0 / 0 / 0 | 0 |
| Where is the per-phase task cap configured, and what is its default? | **2 of 3** (one run reported `{"answer": ""}`, and the contract accepted it) | 69,867 / 115,777 / 103,107 | 17 / 22 / 82 s | 1 / 0 / 5 | 0 |
| Which purposes can an acp agent serve, and where is that enforced? | 3 of 3 (names all five, `table.go:159-165`, `validateStage`, `loadRouteTable`) | 249,203 / 274,864 / 227,235 | 31 / 57 / 127 s | 14 / 11 / 3 | 0 |

- **Wall time, 9 runs:** p50 43.7 s, worst 127.2 s. **Tokens:** p50 103,107, worst 274,864 [run, the 9 runs above]. The token counts include cached tokens; SP-022 (2026-10-21) put about 17,000 of the floor in cache.
- **Cold start, measured separately:** from the start of the jail to the agent's first session update (`available_commands_update`, sent after `initialize` and `session/new`) took 0.69–1.66 s, p50 about 0.79 s [run, 9 runs]. That includes seeding the agent home. Cold start is not the cost; the answer is.
- **Permissions:** every bash request (`ls`, `grep`, `find`, `sed -n`) was allowed by the hooks. Reads never ask. With the workspace read-only, the jail's read-only bind is the real guard, not the permission layer [run, read].
- **Accuracy:** opencode cited line numbers and named keys and an env var (`AGENTD_GATEWAY_MAX_TASKS_PER_PHASE`); spot-checked, all correct [run, 9 runs].

## Direct call (codestral)

`codestral-latest`, `AGENTD_LIVE_DELAY=8s`. Of 32 calls, **21 returned 429** (12 classify, 9 answer) (`LLM provider quota exceeded`, status 429 from Mistral). They are counted as 429s, not as wrong answers. On 2026-10-28 the same model and delay gave 0 errors at 20 runs per case ([acp-agents.md](acp-agents.md#re-run-at-n--20-with-an-8-s-delay-2026-10-28)), so the quota has changed [inferred].

| Probe | Answered | Result | Wall time |
|---|---|---|---|
| Classify, 4 wordings × 5 | 8 of 20 | explain 4/4 and pros/cons 1/1 → `plan_request`; "What does this project do?" 1/1 → `ambiguous`; the cap question 2/2 → `plan_request` | 0.62–0.84 s |
| General answer (SQLite vs Postgres) × 3 | 0 of 3 | all 429 | — |
| Code question with gathered AGENTS.md, README, file list, 3 × 3 | 3 of 9 | Q1 2/2 right; Q3 1/1 "not in the provided files" (honest, unanswered); Q2 0 answered | 0.75–1.01 s, about 3,300 tokens (truncated, fact 6) |

## Recommendation: (c), both, chosen by intent

1. **Board and status questions:** no model call. `StatusSummarizer` already answers "what is blocked and why".
2. **General questions, and questions about failures and memory:** one direct call that answers in text, with data agentd gathers (status, events, the handoff note, recall). This needs a new chat intent and response kind; the classifier has none today. Cost: about 1,000 tokens and about 1 s [inferred]. No tools.
3. **Questions about a project's code:** a text task with `requires_workspace_change: false`, run read-only on the project's routed `exec.turn` agent, with the answer posted to the board like any result. Do not add intake purposes to `stageAgentPurposes`: at p50 44 s and 103,000 tokens (worst 127 s and 275,000) [run, 9 opencode runs], a synchronous chat turn would block on it, and unscoped chat has no workspace to bind anyway [read].
4. **Plan and clarify, and host facts:** unchanged.

**What it costs against a direct call:** for a workspace question, about 30 times the tokens and about 40 times the latency (p50 against p50) [run: 9 opencode runs, 3 `codestral-latest` runs]. For a question that does not need the workspace, the direct call avoids that cost entirely. Planned as a text task after T-070, the same question would cost an agent run, at least the 21,000 floor (SP-022) and 46,000–275,000 measured here [run, 9 opencode runs].

**Why not (a), direct calls only:** a direct call cannot search. Gathering more data than fits in 12,000 chars needs either a larger budget or a model-chosen search, and the second is the start of a tool loop.

**Why not (b), the agent for every question:** it pays 14–83 times the tokens for questions that need no workspace [run: 9 opencode runs against 3 `codestral-latest` runs].

**Thin evidence.** The general-question direct answer has 0 answered runs, and the classifier rows rest on 1–4 answered runs each. The ranking does not depend on them, because the agent's cost alone separates the options. **The one run that would settle the direct-answer leg:** codestral, the general question and the two failure/memory examples, 5 runs each, at a delay of 30 s or more.

## What T-070 should do with text tasks

- **Run a text task read-only.** In `runACPExecution`, take `AccessReadOnly` when `requires_workspace_change` is false, not only when the profile is read-only (fact 4). Size: S; fold it into T-070.
- **Reject an empty result on a text task.** opencode reported `{"answer": ""}` once and the contract accepted it [run, 1 of 9]; cline did twice [run, 2 of 6 answered runs]. The `exec.turn` contract (`acpExecution{Summary}`) has the same shape, so an empty summary would likely pass too [inferred]. Size: XS.
- **Until the answer intent exists, expect each text task to cost an agent run:** 46,000–275,000 tokens and 17–127 s on opencode for read-only questions [run, 9 runs], not the 21,000 floor (SP-022).
- A text task with no `exec.turn` route fails `FAILED_REQUIRES_HUMAN` as T-070 plans [read]; nothing here changes that.

## Follow-up tickets

| ID | What | Size |
|---|---|---|
| T-081 | Chat answer intent: the classifier gains a question intent; one direct call answers general, failure and memory questions from gathered data (status, events, handoff note, recall) within the 12,000-char budget, with a new response kind. No tools. Live test behind `-tags live` | M |
| T-082 | ACP text tasks run read-only: access follows `requires_workspace_change: false` (fold into T-070 if it is not merged yet) | S |
| B-100 | An empty `answer` or `summary` passes the ACP result contract; reject it with the repair prompt (opencode 1 of 9, cline 2 of 6 answered runs) | XS |
| T-083 | Route chat workspace questions to a read-only text task in the project the user names (`approved_scopes`), and post the answer back to the chat or the board | M |
| SP-030 | Done in this spike: the cline preset copies the `cline-pass` (ClinePass) login when present and passes it as `CLINE_PROVIDER`; `lastUsedProvider` is not what ACP sessions read (`presets.go`, `preset_test.go`) | S |
| B-101 | Done in this spike: `ownTool` matched only the exact title `agentd__<tool>`; cline 3.0.74 titles calls `agentd__report_outcome: {args}` and names board tools `agentd__board_list_projects_<8 hex>`, so the driver refused every `report_outcome` and no cline task could finish. It now compares the name before the colon and allows the hash only on agentd's dotted tools (`own_tool.go`, `TestOwnToolNamesAsCline3Sends` with lookalikes) | S |

## What was not checked

- **cline:** 3 of the last 9 runs hit the ClinePass inference cap, so Q3 has no cline answer; tokens are never reported.
- **The general-question direct answer:** 3 of 3 runs were 429s, as were 6 of 9 code-question runs (all 3 for the per-phase cap) and 12 of 20 classify runs.
- **A direct call with a larger budget** than 12,000 chars, or with the file list placed so the head/tail cut keeps it.
- **The full text-task path after T-070** (`dispatchProcess` with the T-067 context pack). The probe used the stage-call path with a short prompt, so a real text task's tokens may be higher.
- The mockllm server and `make test-e2e`; J03, J04 and J13 were read, not run.
- opencode on any other model; agy.
