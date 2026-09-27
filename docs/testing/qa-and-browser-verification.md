# agentd QA Scripts and Manual Browser Verification

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Repeatable API harnesses and
> the manual click-through checklist.

## Chat-to-Kanban privileged handoff QA

The repeatable API portion uses the operator-provided LiteLLM, not the compose
`litellm`. Start from a clean stack (`podman compose -f devenv/compose.yaml down -v`)
if you need a fresh project database, then run:

```bash
AGENTD_API_URL=http://127.0.0.1:8765 \
LITELLM_BASE_URL=http://127.0.0.1:4000/v1 \
LITELLM_API_KEY="$LITELLM_API_KEY" \
LITELLM_MODEL=agentd \
./test/e2e/chat-kanban.sh
```

The script authenticates against `/v1/models`, checks agentd and the web endpoint,
creates the machine-inventory chat plan, materializes it with
`start_empty_workspace`, polls task events for autonomous output, and exercises
the human-resolution API. It exits non-zero when assertions fail and prints the
project/task IDs for investigation. API assertions are automated; browser click-through remains manual.

> **Note:** The script validates that `agentd` is present in the authenticated
> LiteLLM `/v1/models` response and checks agentd/web liveness, but it does **not**
> independently verify that agentd is configured to use the same `LITELLM_BASE_URL`
> and model. That routing is controlled by `devenv/agentd/config.yaml`
> and the compose file. It also leaves the created project and tasks behind.
>
> **Known behavior:** the LiteLLM-backed mock generates task titles like "Set up plan"
> and "Implement core of plan". The script's completion assertion looks for "identity"
> in the task title, which these generic titles do not contain. If the script reports
> "no completed identity task observed", confirm the tasks reached `COMPLETED` via
> `GET /api/v1/projects/{id}/tasks` and treat the script assertion as a mock-title
> mismatch rather than an execution failure.

## Manual browser verification

1. Send the inventory, hardware, and privileged-detail plan in Chat.

2. Approve it once with **Execute Strategy** and confirm root tasks leave
   `PENDING` for `READY`/`QUEUED`/`RUNNING` without a workspace seeding call.

3. Inspect the Board, then ask “How’s it going?”.

4. If an attention card appears, use **Open in Board**, copy the displayed
   command, and run it in the operator’s own terminal.

5. Paste the output into the task drawer’s resolution form and resolve the
   handoff. Confirm the child and parent reach terminal state and do not rerun
   the privileged command. Ask for status again and confirm attention is gone.The browser has no automation; API assertions are automated.

### Sudo/Human-Handoff Edge Cases (not yet covered)

Detection is regex-based and happens in two places: a pre-execution
syntactic block for `sudo` at the start of a command or after `&&`/`||`/`;`/`|`
(`internal/sandbox/executor.go`), and a post-execution output scan
(`internal/queue/safety/permission_detector.go`). Resolution
(`internal/kanban/human_handoff.go` `ResolveHumanHandoff`) accepts **any**
non-empty pasted text as success — it does not re-verify the output. These
edge cases exercise both the detection boundary and the trust boundary of
that resolution step; none are covered by the automated script or the happy
path above.

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
