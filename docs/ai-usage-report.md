# AI usage report

Where do the tokens go? agentd records every model and embedding call at the
gateway under a purpose, and reports them by purpose, stage, model, day, task or
chat request. Records hold sizes, counts, tokens and timing, never prompt or
response bodies.

## What a record is

The router writes one record per **provider attempt**, not per logical call. A
call that fails on one provider and succeeds on the next produces two records. A
call whose JSON answer had to be repaired produces one record per try. Records
go to the `ai_calls` table through an async writer, so the request path never
waits for the database.

| Field | Meaning |
| --- | --- |
| `at`, `latency_ms` | When the attempt started and how long it took |
| `kind` | `generate` or `embed` |
| `purpose`, `stage` | Why the call was made (see below); the stage is the part before the dot |
| `role`, `agent_id`, `task_id` | Model-tier hint, the agent profile, and the task when the call belongs to one |
| `correlation_id` | The chat request's ID (see "Per chat") |
| `provider`, `model` | Who answered; the model the provider reported |
| `attempt` | `first`, or `json_repair` for a retry made to fix an invalid JSON answer |
| `cascade` | True when an earlier provider failed and this attempt is the next one tried |
| `outcome` | `ok`, `failed`, or `invalid_json` (the provider answered but the JSON did not parse) |
| `truncated` | The router cut the input to fit the budget |
| `message_count`, `tool_count`, `input_chars`, `output_chars` | Sizes and counts of what was sent and received |
| `tokens`, `cached_tokens`, `cache_write_tokens` | Provider-reported usage |

`attempt` and `cascade` are separate on purpose. A repair counts against the
purpose that caused it, and a cascade attempt can itself be a first try or a
repair.

## Purposes

The set is closed: 25 purposes in the form `<stage>.<name>`. A call that arrives
with an empty or unknown purpose is stored as `unlabelled` and counted, never
dropped. The minimum level is provisional (SP-023): L1 is a single model call
with no tools or workspace, L3 needs an ACP agent.

| Purpose | Meaning | Min level |
| --- | --- | --- |
| `intake.classify_intent` | Decide whether a chat message is a plan request, a status check, out of scope or ambiguous | L1 |
| `intake.analyze_scope` | Decide whether a chat request is one deliverable or several | L1 |
| `intake.draft_plan` | Draft the phased plan for a chat request | L1 (L3 read-only when the project has code) |
| `create.replan_comment` | Turn a human comment on an IN_CONSIDERATION task into follow-up tasks | L1 (L3 read-only on brownfield) |
| `create.next_phase` | Expand a "Plan Phase N" task into the next phase's tasks | L1 (L3 read-only on brownfield) |
| `create.elicit` | Decide whether a task needs clarifying questions before it runs | L1 |
| `exec.turn` | One iteration of the main tool-calling loop | L3 |
| `exec.work_plan` | Draft the step plan inside an agentic task | L1 |
| `exec.legacy_command` | Legacy one-shot JSON command, including healing-split breakdowns | L1 |
| `exec.batch` | Run several small tasks in one model call | L1 |
| `exec.respec_turn` | Rewrite the user turn after plan validation fails | L1 |
| `exec.repair_section` | Redo one failing plan step's section | L1 |
| `exec.permission_alt` | Propose a non-privileged alternative to a blocked command | L1 |
| `exec.suggest_command` | `agentd suggest`: propose a shell command for a task | L1 |
| `overhead.topic_drift` | YES/NO: did the session topic change | L1 |
| `overhead.compact_context` | Summarize old turns to fit the context budget | L1 |
| `overhead.subagent_turn` | One step of a delegated subagent loop | L3 |
| `overhead.embed_files` | Embed files and queries to pick relevant file context | L1 |
| `maint.summarize_log` | Map and reduce passes over a task's execution log | L1 |
| `maint.extract_memory` | Turn the condensed log into a symptom/solution memory | L1 |
| `maint.merge_memories` | Merge a cluster of similar memories into one | L1 |
| `hidden.truncate_summary` | Gateway truncator summarizes an oversized message | L1 |
| `system.warmup` | Daemon start ping | n/a |
| `legacy.contract_text` | `GenerateText`, no production caller; removed by T-055 | drop |
| `legacy.contract_json` | `GenerateStructuredJSON` generic branch, unreachable in production; removed by T-055 | drop |
| `unlabelled` | Reserved: any call that arrives with an empty or unknown purpose | n/a |

`system.warmup` is one row per daemon start and is part of the totals;
`legacy.*` rows should stay at zero.

## Reading the report

```bash
agentd report ai                                  # by purpose, all time
agentd report ai --group-by stage,model --from 2026-10-18
agentd report ai --group-by task --stage exec     # tokens per task
agentd report ai --group-by purpose --request <correlation-id> --json
```

The command reads `GET /api/v1/reports/ai-usage` from the running daemon, so
start the daemon first. Output is one line per group, a `TOTAL` line, and the
dropped-record count:

| Column | Meaning |
| --- | --- |
| `CALLS` | Provider attempts in the group (every record, including repairs and cascade attempts) |
| `TOKENS`, `CACHED` | Summed provider-reported tokens, and the cached part |
| `AVG_MS`, `MAX_MS` | Average and maximum attempt latency. There are no percentiles: SQLite has no percentile aggregate |
| `REPAIR%` | `json_repair` attempts divided by first attempts. High means the purpose's JSON contract is fragile |
| `CASCADE%` | Attempts made after a provider failed, divided by all attempts. High means the first provider is unreliable for this purpose |
| `FAILED` | Attempts whose outcome is `failed` |

Dimensions for `--group-by` (comma list, default `purpose`): `purpose`, `stage`,
`model`, `day` (UTC date), `task`, `request`. Filters: `--from` (inclusive),
`--to` (exclusive; a bare date includes that whole day), `--task`, `--request`,
`--purpose`, `--stage`, `--model`. Dates are `YYYY-MM-DD` or RFC3339.

The questions the report is built to answer:

- **Per chat request:** `--group-by request`, or `--request <id> --group-by purpose`.
- **Per task:** `--group-by task`, or `--task <id> --group-by purpose`.
- **Per day:** `--group-by day`, or `day,purpose` for the split.

"Per chat" means per chat request: one `POST /v1/chat/completions` gets one
correlation ID, and every call made while serving it (intent, scope, draft
plan, repairs, cascade attempts) carries it. A multi-turn conversation is
several requests. A worker's later calls for a task the chat created carry that
task's ID and no chat correlation ID, so use `--task` to follow them.

## What the context is made of

`agentd report ai --mix` swaps the token columns for the input's composition.
Each record carries the input size by section (chars and message counts, never
bodies); the report sums chars per section and shows each as a share of
`INPUT_CHARS`.

| Column | Section | What it holds |
| --- | --- | --- |
| `SYSTEM%` | `system` | System prompt, house rules, project instructions, skills |
| `MEMORIES%` | `memories` | Recalled lessons and saved user preferences |
| `TASK%` | `task` | Task title and description (the first user message) |
| `PARENT%` | `parent_outcome` | Results of parent tasks. Only the tiered path builds these; expect 0 for `exec.turn` |
| `COMMENTS%` | `comments` | Feedback added mid-task: human comments, corrections, review rejections, sandbox feedback |
| `FILE%` | `file_context` | Results of the `read` tool, which the file pipeline feeds |
| `HISTORY%` | `history` | Earlier assistant turns and compressed summaries |
| `TOOL%` | `tool_results` | Output of every other tool |

A message the builder did not label falls back to its role (`tool` → tool
results, `assistant` → history, the first `user` → task, a later `user` →
comments, anything else → system). Rows recorded before this column existed
count as no mix, so shares only cover calls that have one.

**Which layer cut the input.** `CUT%` is the share of calls whose input was
shortened before the provider saw it. The next four columns count calls cut by
each layer (a call can be cut by more than one):

| Column | Layer | Limit |
| --- | --- | --- |
| `ZONE` | Working zone, in `PrepareContext` | `queue.agentic_context` (budgets sum to the zone total) |
| `BUDGET` | Character budget with the configured strategy | `queue.agentic_character_budget`, which inherits `gateway.truncator.max_input_chars` (12,000) when 0; the strategy is `gateway.truncation.strategy` |
| `HEAD` | Agentic head cut | the same character budget, plus `queue.agentic_truncator_max` messages |
| `ROUTER` | The gateway's own truncator | `gateway.truncator.max_input_chars`; agentic calls skip it |

Compare `INPUT_CHARS` per call with the two limits: with product defaults the
12,000-character layers run before a 60,000-character working zone has anything
to cut, so `ZONE` stays at 0 and `gateway.truncation.strategy` only applies where
`BUDGET` shows up. The report states what happened; it does not change a limit.

## Tool use and repeated reads

```bash
agentd report tools                         # all tasks
agentd report tools --task <id>             # one task
agentd report tools --from 2026-10-20
```

One row per tool dispatch goes into `tool_calls` (task, tool, file path for
file tools, ok or failed, result bytes; no arguments or output). The TOOL_CALL
and TOOL_RESULT events cannot be the source: they keep only 200 characters of
arguments and are deleted once a task is curated.

| Output | Meaning |
| --- | --- |
| `TOOL`, `CALLS`, `FAILED`, `RESULT_BYTES` | Per tool: dispatches, those that did not end `ok`, bytes returned |
| `re-read: X of Y bytes read` | Bytes the second and later reads of the same file added, over all bytes `read` returned |
| `TASK`, `PATH`, `READS`, `BYTES`, `REREAD_BYTES` | The 20 files with the most re-read bytes, per task |

A repeated read is the same `read` path more than once in one task. A `cat`
inside `bash` counts as a bash call, not a read, and a different spelling of the
same path counts as another file. Nothing here deduplicates reads; it is the
baseline for deciding whether that is worth building.

## Known limits

- **Empty task ID.** Topic guard, subagent, librarian, dream and truncator
  summary calls have no `task_id` today, so they show up under their purpose with
  an empty task and are missing from a per-task view.
- **Dropped records.** The writer holds 1,024 records and inserts in batches of
  100 or every 2 seconds. When the buffer is full, or a batch fails to insert,
  the records are dropped and counted. `dropped_records` is the count since the
  daemon started and resets on restart; a persistent non-zero value means the
  database is too slow or full.
- **No retention.** `ai_calls` and `tool_calls` grow without bound. A retention setting is a
  follow-up, not part of this change.
- **Gateway calls only.** Usage by external CLI agents (SP-022) will be
  self-reported and will not match gateway-measured numbers exactly.
- **Provider-reported tokens.** A provider that omits usage records 0, and
  embedding calls carry no tokens. A failed attempt has no model.

The same report is `GET /api/v1/reports/ai-usage`, and the tool use report is
`GET /api/v1/reports/tool-use`; see
[api-testing.md](api-testing.md#get-apiv1reportsai-usage).
