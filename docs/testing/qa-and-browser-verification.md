# agentd QA Scripts and Manual Browser Verification

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Repeatable API harnesses and
> the manual click-through checklist.

## Chat-to-Kanban privileged handoff QA

This used to be a standalone shell script (`test/e2e/chat-kanban.sh`, before
that `scripts/chat-kanban-qa.sh`). It is **deleted**: everything it asserted is
now covered by the e2e journey suite, which runs the same flow against the
compose stack and fails loudly instead of exiting non-zero from a `jq` pipeline.

| What the script did | Where it lives now |
| --- | --- |
| Authenticated `/v1/models` and checked `agentd` was present | `TestJ13_OpenAIIntake` (`test/e2e/journeys_openai_test.go`) — the same compose litellm the script pointed at, so the routing assertion is no longer needed: the journey's requests go through the daemon's own provider config |
| Created a plan, materialized it, polled task events to autonomous output | `TestJ04_FullHappyPath` — chat → plan → materialize → workspace/ready → all tasks COMPLETED |
| Exercised the human-resolution API / privileged handoff | `TestJ07_HealingHandoff` (healing profile) — connector failure opens the breaker, producing a BLOCKED parent and a "Manual review required:" HUMAN child |

Run them all with:

```bash
make dev-clean dev-up   # the journeys run against the compose stack
make test-e2e
```

Or one journey:

```bash
go test -v -tags=e2e -count=1 -run 'TestJ04|TestJ07' ./test/e2e/
```

What is **not** covered, and is why the browser checklist below is still
manual: nothing here renders the UI. The privileged-handoff click-through
(steps 1-5) and the sudo edge cases (1-4) have no automated coverage.

## Manual browser verification

1. Send the inventory, hardware, and privileged-detail plan in Chat.

2. Approve it once with **Execute Strategy** and confirm root tasks leave
   `PENDING` for `READY`/`QUEUED`/`RUNNING` without a workspace seeding call.

3. Inspect the Board, then ask “How’s it going?”.

4. If an attention card appears, use **Open in Board**, copy the displayed
   command, and run it in the operator’s own terminal.

5. Paste the output into the task drawer’s resolution form and resolve the
   handoff. Confirm the child and parent reach terminal state and do not rerun
   the privileged command. Ask for status again and confirm attention is gone. The browser has no automation; the API assertions above are automated.

### Sudo/Human-Handoff Edge Cases (not yet covered)

Detection is regex-based and happens in two places: a pre-execution
syntactic block for `sudo` at the start of a command or after `&&`/`||`/`;`/`|`
(`internal/sandbox/executor.go`), and a post-execution output scan
(`internal/queue/safety/permission_detector.go`). Resolution
(`internal/kanban/human_handoff.go` `ResolveHumanHandoff`) accepts **any**
non-empty pasted text as success — it does not re-verify the output. These
edge cases exercise both the detection boundary and the trust boundary of
that resolution step; none are covered by the journey suite or the happy path
above.

> **Note:** Earlier versions of these paths had minimal daemon stdout logging.
> The current codebase now emits `slog` calls in `permission_detector.go`,
> `worker_permission.go`, `human_handoff.go`, and `sandbox/executor.go`, so
> handoff and permission events should be visible in daemon logs as well as
> task events and SSE. Still verify with `podman compose logs -f agentd` in
> parallel with API/SSE checks.

1. **Bypass check — `sudo` inside a subshell or heredoc.** Ask the agent to
   run something like `echo "$(sudo whoami)"` or a multi-line script with
   `sudo` on its own line after a newline (not after `&&`/`;`/`|`). Confirm
   whether the sandbox actually blocks it — the current regex only matches
   `sudo` at the start of a command or immediately after a shell operator,
   so a subshell or bare-newline occurrence may execute without triggering
   the human-handoff flow at all. **Do not run this in production; use a
   disposable workspace.** The current Alpine runtime does not install `sudo`,
   so treat this as a parser/escape-boundary test rather than a
   privilege-escalation demo.

2. **Wrong/garbage password pasted back.** Trigger a real sudo handoff, then
   resolve it with plainly wrong text (e.g. "asdf" or the literal string
   "done") instead of real command output. Confirm the task is marked
   successful anyway (this is expected given current code — the resolution
   endpoint does not validate the pasted result), and note this as a trust
   boundary the operator must self-police.

3. **Multiple concurrent sudo subtasks on one parent.** Ask for a plan whose
   steps require sudo more than once (e.g. two different privileged
   commands). Confirm both attention cards appear, `countOpenHandoffSiblings`
   correctly tracks that more than one sibling is open, and resolving one
   does not prematurely unblock the parent while the other is still pending.

4. **Handoff timeout expiry.** The default legacy handoff timeout is 7 days
   (`internal/config/queue.go` `DefaultLegacyHandoffTimeout`). Full expiry is
   impractical to test in real time — instead, verify (via code/config, or a
   shortened timeout in a test config) that an expired handoff transitions
   the task to a clear failed/expired state rather than hanging forever, and
   that the transition is visible in the Board and via
   `GET /api/v1/tasks/{id}/events`.

All checkpoints were verified end-to-end against the running stack
(`podman compose -f devenv/compose.yaml up --build -d`) using
LiteLLM as the LLM provider (model `agentd`). Verified 2026-09-25.

| Checkpoint | Result | Evidence |

|---|---|---|

| CP0 – Daemon boot + LLM connectivity | ✅ PASS | Isolated warmup: `tool credentials validated` → `running LLM warmup` → `LLM warmup OK provider=litellm model=agentd` → `API server listening`; warmup failure → hard exit code 1 with `command failed` summary; `--skip-llm-warmup` → no warmup lines, boots straight to `API server listening` |

| CP1 – Logs + Kanban accessible | ✅ PASS | `GET /` → HTTP 200; `GET /api/v1/projects` → 0 projects; SSE endpoint accepts connections |

| CP2 – Chat interface works | ✅ PASS | `POST /v1/chat/completions` returns `create_plan` tool call with DraftPlan via LiteLLM |

| CP3 – Kanban reflects database | ✅ PASS | `POST /api/v1/projects/materialize` → tasks visible via task API; root tasks `READY`; invalid event limits → 400 |

| CP4 – Multi-task plan + execution | ✅ PASS | Materialized plan tasks reached `COMPLETED`; `PLAN_RESULTS.log` exists in project workspace |
