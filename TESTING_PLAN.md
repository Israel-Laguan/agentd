# agentd Testing Plan

This document outlines the testing plan to verify agentd functionality through the web interface.

## Prerequisites

- Podman (or Docker) and Podman Compose installed
- Ports 3000, 4000, 8765 available

## Quick Start with Podman Compose

The easiest way to run the full stack locally:

```bash
# Build and start all services
podman compose -f docker-compose.dev.yml up --build -d

# Open http://localhost:3000

# To stop
podman compose -f docker-compose.dev.yml down

# To rebuild after code changes:
podman compose -f docker-compose.dev.yml build --no-cache
```

> **Note:** If the web container fails to start with `Cannot find module '@tailwindcss/postcss'`,
> ensure the `web` service has `NODE_ENV: "development"` set in `docker-compose.dev.yml`.
> The node:22-alpine image ships `NODE_ENV=production`, which causes `npm install` to skip
> devDependencies. With `NODE_ENV=development`, the full dependency tree (including devDeps
> like `@tailwindcss/postcss`) is installed before `next dev` starts.
>
> **Note:** The `agentd` service in this compose file starts with `--skip-llm-warmup`.
> Because of that, a green `/health` or `/api/v1/system/status` does **not** prove LiteLLM
> connectivity. See Checkpoint 0 for the isolated warmup run that does prove connectivity.

Services:
- **litellm** (port 4000) - LLM proxy/router
- **agentd** (port 8765) - Daemon
- **web** (port 3000) - Next.js frontend

## Running the Test Environment (Manual)

The compose stack is the recommended way to run the full local test environment
(`podman compose -f docker-compose.dev.yml up --build -d`). It wires agentd
through LiteLLM as the LLM provider.

### Terminal 1: Start LiteLLM (compose handles this)
```bash
podman compose -f docker-compose.dev.yml up -d litellm
```

### Terminal 2: Start agentd daemon
Build first:
```bash
make build
```
Then run with a config whose gateway provider points at LiteLLM and
`warmup_enabled: true` if you want to prove LLM connectivity (Checkpoint 0).
With `--skip-llm-warmup` the daemon boots without proving provider connectivity.

### Terminal 3: Start web frontend
```bash
cd web
NEXT_PUBLIC_USE_MOCK=false npm run dev
```

### Terminal 4: Access the web interface
Open browser to: **http://localhost:3000**

---

## Test Checkpoints

### Checkpoint 0: Daemon Boot Sequence and LLM Connectivity

Boot order is: seed default agent → provider presence check → tool-credential
validation → LLM warmup → HTTP listener bind → daemon/queue start
(`cmd/agentd/start.go`, `start_serve.go`, `start_runtime.go`). This checkpoint
exists because the provider "health" check at boot (`internal/config/health.go`
`tryAPIKeyHealth`) only verifies an API key string is non-empty — it does
**not** make a network call. Real connectivity is only proven by the LLM
warmup step, which is skippable.

Use `-v` (verbose/debug logging, as in the manual Terminal 2 command) so the
step-by-step boot sequence is actually visible — without it, several of the
milestone lines below are emitted at `slog.Debug` and won't show.

**Steps:**
1. With litellm running, start agentd **without** `--skip-llm-warmup` and
   `-v`, and watch the log stream (or `podman logs -f agentd_agentd_1`) for
   the boot sequence in order.
2. Stop litellm, then start agentd **without** `--skip-llm-warmup`.
3. Stop litellm, then start agentd **with** `--skip-llm-warmup` (or
   `gateway.warmup_enabled: false`).
4. With the daemon from step 3 running (litellm still down), send a chat
   message and observe the failure mode, in both the API response and logs.

**Expected Results — this is the primary way to confirm the loop actually
started, not just that the process is alive:**
- Step 1: logs show, in order — `"running LLM warmup"` (debug) →
  `"LLM warmup OK" provider=... model=...` (info,
  `internal/config/health_warmup.go`) → `"API server listening"
  address=...` (info, `cmd/agentd/start.go`) → `"HTTP server started"`
  (debug) → `"starting daemon"` (debug). `/api/v1/system/status` shows
  provider metadata, but **does not prove provider connectivity**; the warmup
  step is the only boot-time proof.
- Step 2: boot **fails hard** — this is not a soft-fail. `warmupLLMIfNeeded`
  wraps the failure in `config.ErrLLMWarmup`, `seedAndValidateStartup`
  returns it before the listener ever binds, and `reportCommandError`
  (`cmd/agentd/errors.go`) prints both a human summary ("agentd reached your
  LLM provider, but the startup warmup failed...") and
  `slog.Error("command failed", "summary", ..., "error", ...)` to stderr, then
  the process exits non-zero. Confirm no `"API server listening"` line ever
  appears and the process actually exits (don't mistake a hang for a fail-fast).
- Step 3: **known gap** — no `"LLM warmup"` lines appear at all (skipped
  before the call), boot proceeds straight to `"API server listening"`, and
  the daemon reports healthy even though litellm is unreachable. Confirm this
  is in fact what happens (don't assume) — the logs will look identical to a
  healthy boot.
- Step 4: chat/task calls fail only at first real use. Confirm the failure
  surfaces in **both** places: the API response/chat UI, and a daemon log
  line (the gateway call site should log the provider error — grep for
  `error` around the timestamp of the failed request). If no log line
  appears at all for this failure, that itself is a gap worth flagging.

> **Compose note:** `docker-compose.dev.yml` starts agentd with `--skip-llm-warmup`,
> so the Quick Start stack does **not** exercise Step 1/2 above. Use the isolated
> warmup run described below (or override the entrypoint) to prove provider connectivity.

**API verification:**
```bash
curl -s http://localhost:8765/health
curl -s http://localhost:8765/api/v1/system/status | python3 -m json.tool
curl -s http://localhost:8765/api/v1/gateway/providers | python3 -m json.tool

# log-based verification (adjust container/binary name as needed)
podman logs agentd_agentd_1 2>&1 | grep -E 'tool credentials validated|running LLM warmup|LLM warmup OK|API server listening|command failed'
```

---

### Checkpoint 1: Verify Logs Appear When Loop Starts and Kanban is Accessible

**Steps:**
1. Open browser to http://localhost:3000
2. Check if the Kanban board loads
3. Look for logs panel/section
4. Verify the daemon logs are visible

**Expected Results:**
- Kanban board displays with task columns
- The Logs panel shows a status dot and daemon/event stream activity
- No console errors
- **Known gap:** the Logs panel currently always displays `System Live` and uses a
  blue disconnected dot rather than a red one. Normal task logs are not surfaced in
  the daemon-wide Logs panel; use `GET /api/v1/tasks/{id}/events` for durable,
  per-task evidence.

**API verification (automated alternative to visual check):**
```bash
curl -s http://localhost:8765/health
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool
curl -s http://localhost:8765/api/v1/system/status | python3 -m json.tool
curl -s http://localhost:8765/api/v1/gateway/providers | python3 -m json.tool
curl -s -N http://localhost:8765/api/v1/events/stream
```

> **Note:** `/health` is a plain liveness probe; `/api/v1/system/status` is a
> richer readiness view (includes provider/queue state). Check both — a
> service can be "alive" (`/health` 200) while its LLM provider is actually
> unreachable (see Checkpoint 0).

#### Checkpoint 1b: Web Client Connection Awareness (known gap)

The web app has **no** dedicated health check against the daemon. The only
"connected" signal in the UI is the SSE stream's `onopen`/`onerror`, and it
only drives the Logs panel's status dot — chat has no equivalent indicator.
A chat-side failure (daemon down, or LLM down) surfaces only as a generic
"Sorry, I encountered an error..." message, indistinguishable from any other
failure.

**Steps:**
1. Load the web app with the daemon already stopped. Observe the chat panel
   and the Logs panel status dot.
2. With the app already loaded and connected, stop the daemon mid-session,
   then send a chat message.

**Expected Results (documenting current behavior, not asserting correctness):**
- Step 1: Logs panel shows disconnected (red dot via SSE `onerror`); chat
  input gives no explicit "not connected" cue before the user tries to send.
- Step 2: chat shows a generic error message with no distinction between
  "daemon unreachable" and "LLM unreachable" — flag this as a known UX gap,
  not a regression, unless it changes.

---

### Checkpoint 2: Chat Interface Works

**Steps:**
1. Find the chat input in the web interface
2. Type a message (e.g., "Build a holiday card reminder app")
3. Send the message

**Expected Results:**
- Message appears in chat history
- Response from the agent appears (a `create_plan` tool call with a DraftPlan)
- No HTTP errors in console
- **Note:** the `model` field in the chat request is echoed in the response; it does
  **not** select agentd's gateway provider. Provider routing is controlled by
  `docker-compose.dev.yml` / agentd config (`gateway.order` and `gateway.providers`).

**API verification:**
```bash
curl -s -X POST http://localhost:8765/v1/chat/completions   -H "Content-Type: application/json"   -d '{"model":"agentd","messages":[{"role":"user","content":"Build a holiday card reminder app"}],"tools":[{"type":"function","function":{"name":"create_plan"}}]}'
```

#### Checkpoint 2b: Chat Agent Works Independently (No Plan Created)

Plain conversational chat is routed entirely separately from task/plan
handling (`internal/api/controllers/chat.go` → `frontdesk.Planner.PlanContent`).
A tool call is only emitted when the planner's response contains a
`status_report` or a non-empty `tasks[]`; otherwise the reply is plain
content with no side effects. This path is not exercised anywhere in the
existing checkpoints, which all use plan-triggering prompts.

**Steps:**
1. Note current project/task counts via the API verification below.
2. Send a message with no action/build keywords, e.g. "What can you help me
   with?" or "Tell me the current laptop OS and hardware" phrased as a
   question rather than a task request.
3. Re-check project/task counts and the SSE stream.

**Expected Results:**
- Agent replies conversationally; response contains no `create_plan` tool call.
- No new project or task rows are created.
- No task-related SSE events are emitted (`task_created`, `task_retried`, etc.).

**API verification:**
```bash
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool  # before
curl -s -X POST http://localhost:8765/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"agentd","messages":[{"role":"user","content":"What can you help me with?"}]}'
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool  # after — should be unchanged
```

### Checkpoint 3: Kanban View Reflects Database

**Steps:**
1. Create a task via chat (Checkpoint 2)
2. Approve the plan (in the web UI, this triggers `POST /api/v1/projects/materialize` with `start_empty_workspace: true`)
3. Check the Kanban board
4. Verify task appears in correct column

**Expected Results:**
- Tasks created via chat appear in Kanban
- Task states (PENDING, RUNNING, COMPLETED, etc.) are correct
- Data matches what's in SQLite database

**API verification:**
```bash
PROJECT_ID="replace-with-project-id"
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool
curl -s "http://localhost:8765/api/v1/projects/${PROJECT_ID}/tasks" | python3 -m json.tool

# The agentd image does not ship the `sqlite3` CLI. Use the task API or query the
# database from a separate utility container if direct SQL is needed.
```

> **Note:** The database lives in the named volume `agentd-data` at `/home/agentd/global.db`.

#### Checkpoint 3a: Per-Task Event Log (Task Drawer)

Distinct from the daemon-wide Logs panel (Checkpoint 1's SSE stream), each
task has its own event history introduced alongside the human-handoff
feature: `GET /api/v1/tasks/{id}/events` (`internal/api/controllers/tasks_events.go`)
feeds the `TaskEventList` component in the Task Drawer
(`web/app/components/task/task-event-list.tsx`). It scrubs sensitive content
from payloads (`sandbox.NewScrubber`), truncates any payload over 16KB
(`maxTaskEventPayload`, sets `payload_truncated: true`), and paginates via a
`limit` query param (default 100, max 200).

**Steps:**
1. Open a task's drawer in the Board after it has gone through several
   lifecycle transitions (created → running → blocked/handoff → resolved).
   Confirm the event list shows one entry per transition, newest first, each
   expandable to its payload.
2. Trigger a task whose output/payload would contain something scrub-worthy
   (e.g. an env var or secret-looking string in command output) and confirm
   the scrubber redacts it in the displayed payload — not just in daemon logs.
3. Trigger or synthesize an event with a payload larger than 16KB and confirm
   the UI shows the "Payload truncated" warning and the API's
   `payload_truncated: true` flag.
4. Call the events endpoint with an out-of-range `limit` (e.g. `limit=0` or
   `limit=500`) and confirm a clean 400 validation error, not a silent
   clamp or crash.

**Expected Results:**
- Event list matches the task's actual lifecycle (task creation, permission
  handoff, human resolution, retries, terminal state) with no gaps or
  duplicates.
- No secret/sensitive payload content reaches the browser unscrubbed.
- Truncation flag and warning UI behave as coded.
- Invalid `limit` is rejected with a clear error.

**API verification:**
```bash
TASK_ID="replace-with-task-id"
curl -s "http://localhost:8765/api/v1/tasks/${TASK_ID}/events" | python3 -m json.tool
curl -s "http://localhost:8765/api/v1/tasks/${TASK_ID}/events?limit=0"    # expect 400
curl -s "http://localhost:8765/api/v1/tasks/${TASK_ID}/events?limit=500"  # expect 400 (max 200)
```

---

#### Checkpoint 3b: Plan Materialization Edge Cases

`ProjectService.MaterializePlan` (`internal/services/project_service.go`) has
two flows — `source_path` (seed synchronously, tasks unlock to READY) and
`start_empty_workspace` (tasks stay PENDING until `POST workspace/ready`) —
each with validation the happy-path checkpoints above don't exercise.

**Steps and expected results:**
1. Call `POST /api/v1/projects/{id}/workspace/ready` on a project whose
   workspace has **not** been seeded yet (no file written). Expect an error
   (`ErrWorkspaceNotReady`), not a silent success — tasks must remain PENDING.
2. Materialize a plan with `source_path` pointing at a nonexistent or
   unreadable directory. Expect materialization to return an error. Note that
   project/task rows may already be persisted before the error is returned, so
   the failure is not always fully atomic; inspect `GET /api/v1/projects` after
   the call if atomicity matters.
3. Call `workspace/ready` twice in a row after a valid seed. Expect the
   second call to be a safe no-op (or a clear "already ready" response), not
   a duplicate dispatch of already-READY tasks.

```bash
PROJECT_ID="replace-with-project-id"
curl -s -X POST "http://localhost:8765/api/v1/projects/${PROJECT_ID}/workspace/ready"   # before seeding — expect error
curl -s -X POST http://localhost:8765/api/v1/projects/materialize -H "Content-Type: application/json" \
  -d '{"source_path":"/nonexistent/path", ...}'   # fill in real plan fields; expect clean failure
```

#### Known Gap: Refining an Overly Broad Plan

There is currently **no** refine/narrow-scope mechanism in the code
(`models/plan.go`'s `DraftPlan` has no version or parent-plan linkage, and no
"revise" endpoint exists). Pushing back on a plan in chat produces a brand
new `DraftPlan` from scratch rather than a narrowed revision of the original.

**Steps:**
1. Send a deliberately broad request, e.g. "Build me a full SaaS product."
2. Push back in the same chat thread: "That's too broad, just do the landing
   page for now."
3. Compare the second `DraftPlan` to the first.

**Expected Results (document actual behavior — this is a known limitation,
not a pass/fail check):** the second plan is an independent draft with no
explicit link back to the first; there is no diff/narrowing UI. If this
changes in a future release, promote this from "known gap" to a real
pass/fail checkpoint.

---

### Checkpoint 4: Chat Creates Tasks in Kanban and Logs Populate

**Steps:**
1. Send a request to create multiple tasks via chat
2. Example: "Create tasks for: (1) Write documentation, (2) Fix login bug, (3) Add dark mode"
3. **Approve the returned plan** (in the web UI this calls `POST /api/v1/projects/materialize`;
   for deterministic hierarchy use the API directly with `start_empty_workspace: true`)
4. Watch the Kanban board for new tasks
5. Observe the logs panel / task events

**Expected Results:**
- After approval, root tasks leave `PENDING` for `READY`/`RUNNING`; dependent
  tasks stay `PENDING` until their dependencies complete.
- Agent processes tasks (moves to RUNNING)
- Per-task events and `PLAN_RESULTS.log` show execution details
- Tasks eventually reach a terminal state (`COMPLETED`, `FAILED`, or
  `FAILED_REQUIRES_HUMAN`)
- **Known gap:** approval through the web UI can lose task metadata such as
  `depends_on`, `assignee`, and `success_criteria`. Use `POST /api/v1/projects/materialize`
  directly when you need the full task hierarchy preserved.

**API verification:**
```bash
# 1. Create a plan
curl -s -X POST http://localhost:8765/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"agentd","messages":[{"role":"user","content":"Create tasks for: (1) Write documentation, (2) Fix login bug, (3) Add dark mode"}],"tools":[{"type":"function","function":{"name":"create_plan"}}]}'

# 2. Materialize with start_empty_workspace (approval)
curl -s -X POST http://localhost:8765/api/v1/projects/materialize -H "Content-Type: application/json" \
  -d '{"project_name":"test","description":"test","start_empty_workspace":true,"tasks":[...]}'

PROJECT_ID="replace-with-project-id"
curl -s "http://localhost:8765/api/v1/projects/${PROJECT_ID}/tasks" | python3 -m json.tool

# 3. Evidence: per-task events and PLAN_RESULTS.log
TASK_ID="replace-with-task-id"
curl -s "http://localhost:8765/api/v1/tasks/${TASK_ID}/events" | python3 -m json.tool
podman compose exec agentd cat "/home/agentd/projects/${PROJECT_ID}/PLAN_RESULTS.log"
```

---

## Chat-to-Kanban privileged handoff QA

The repeatable API portion uses the operator-provided LiteLLM, not the compose
`litellm`. Start from a clean stack (`podman compose -f docker-compose.dev.yml down -v`)
if you need a fresh project database, then run:

```bash
AGENTD_API_URL=http://127.0.0.1:8765 \
LITELLM_BASE_URL=http://127.0.0.1:4000/v1 \
LITELLM_API_KEY="$LITELLM_API_KEY" \
LITELLM_MODEL=agentd \
./scripts/chat-kanban-qa.sh
```

The script authenticates against `/v1/models`, checks agentd and the web endpoint,
creates the machine-inventory chat plan, materializes it with
`start_empty_workspace`, polls task events for autonomous output, and exercises
the human-resolution API. It exits non-zero when assertions fail and prints the
project/task IDs for investigation. API assertions are automated; browser click-through remains manual.

> **Note:** The script validates that `agentd` is present in the authenticated
> LiteLLM `/v1/models` response and checks agentd/web liveness, but it does **not**
> independently verify that agentd is configured to use the same `LITELLM_BASE_URL`
> and model. That routing is controlled by `deploy/docker-plan-execute/agentd/config.yaml`
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
(`podman compose -f docker-compose.dev.yml up --build -d`) using
LiteLLM as the LLM provider (model `agentd`). Verified 2026-09-25.

| Checkpoint | Result | Evidence |
|---|---|---|
| CP0 – Daemon boot + LLM connectivity | ✅ PASS | Isolated warmup: `tool credentials validated` → `running LLM warmup` → `LLM warmup OK provider=litellm model=agentd` → `API server listening`; warmup failure → hard exit code 1 with `command failed` summary; `--skip-llm-warmup` → no warmup lines, boots straight to `API server listening` |
| CP1 – Logs + Kanban accessible | ✅ PASS | `GET /` → HTTP 200; `GET /api/v1/projects` → 0 projects; SSE endpoint accepts connections |
| CP2 – Chat interface works | ✅ PASS | `POST /v1/chat/completions` returns `create_plan` tool call with DraftPlan via LiteLLM |
| CP3 – Kanban reflects database | ✅ PASS | `POST /api/v1/projects/materialize` → tasks visible via task API; root tasks `READY`; invalid event limits → 400 |
| CP4 – Multi-task plan + execution | ✅ PASS | Materialized plan tasks reached `COMPLETED`; `PLAN_RESULTS.log` exists in project workspace |

## Execution results and notes

### CP0 verification
- Isolated warmup run succeeded: `tool credentials validated` → `running LLM warmup` → `LLM warmup OK` → `API server listening` → `starting daemon`.
- Warmup failure run confirmed hard exit (code 1) with `command failed` summary when litellm was stopped.
- Compose default (`--skip-llm-warmup`) boots to `API server listening` with **no** warmup lines, as expected.

### CP3b edge cases
- `workspace/ready` before seeding returned `409` / `ErrWorkspaceNotReady` with tasks staying `PENDING`.
- Nonexistent `source_path` materialization returned an error and did **not** persist the project row — failure is atomic.
- `workspace/ready` twice after a valid seed was safe (second call succeeded without duplicating dispatch).

### Chat-to-Kanban QA script
- `./scripts/chat-kanban-qa.sh` created a project, materialized it with `start_empty_workspace: true`, and tasks reached `COMPLETED`.
- The script then timed out waiting for a human-attention task. This is expected with `healing.enabled: false` in `docker-compose.dev.yml`; no handoff/attention flow was triggered. Do not treat this as a script failure unless a real-provider run with healing enabled is intended.
- Project/task IDs for investigation: `ca0cdbd4-c56a-427e-9c3e-794fcfd13acd`.
- The stack now uses LiteLLM with model `agentd` (previously `mock/agentd`). The
  `/v1/models` endpoint returns `agentd` and agentd's provider config references
  `model: "agentd"`.

### Known gaps confirmed live
- Web Logs panel always shows `System Live`; task logs are not in the daemon-wide Logs panel.
- `sqlite3` CLI is not installed in the agentd image; use task APIs or a separate utility container.
- The compose stack starts agentd with `--skip-llm-warmup`, so `/health` does not prove provider connectivity.
- `POST /api/v1/projects/materialize` with nonexistent `source_path` returns error without persisting state.

Additional observations:

- **Web nav routes:** `/board` and `/logs` return 404 — this is expected. The sidebar uses
  client-side `<button>` navigation (single-page tab state), not Next.js file routes. Only `/`
  exists as a route.
- **SSE firehose verified live:** `GET /api/v1/events/stream` emitted frames during a task
  retry:
  ```
  event: task_retried
  event: token_usage
  event: poison_pill_handoff
  ```
  The stream is silent when no state changes occur — that is expected behavior, not a bug.
- **Task execution reaches terminal states:** tasks end as `QUEUED` (in flight) or
  `FAILED_REQUIRES_HUMAN` after auto-retries. See troubleshooting below for why.

---

## Troubleshooting

### Tasks stuck in PENDING (never dispatched)

Materialized plans **without** a `source_path` or explicit
`start_empty_workspace: true` intentionally create tasks in `PENDING` and keep
them unclaimable until an operator signals workspace readiness (see
`ProjectService.MaterializePlan` in `internal/services/project_service.go`). To unlock:

```bash
PROJECT_ID="replace-with-project-id"

# 1. Seed at least one file into the project workspace (IsWorkspacePopulated requires >=1 entry)
podman exec agentd_agentd_1 sh -c 'echo "# workspace" > "/home/agentd/projects/$1/README.md"' sh "$PROJECT_ID"

# 2. Transition PENDING -> READY (dispatch happens within ~10s after this)
curl -s -X POST "http://localhost:8765/api/v1/projects/${PROJECT_ID}/workspace/ready"
```

Chat-created plans now send `start_empty_workspace: true`, so they use the
explicit empty-workspace flow. Plans using the two-phase seeded-workspace flow
must omit the flag, seed content, and then call `workspace/ready`.

Note: the boot log field `scheduler_enabled: false` refers to the optional **agentic cron
scheduler** (`agentic.scheduler.enabled`, defaults to `false`) — it is unrelated to normal task
dispatch. The queue daemon's `taskLoop` runs regardless.

### Tasks fail with FAILED_REQUIRES_HUMAN / poison_pill_handoff

After auto-retries, an executable task can end in `FAILED_REQUIRES_HUMAN`. Inspect the daemon
logs and the project workspace before retrying. A sandbox path violation means the persisted
`workspace_path` is stale or outside the configured `projects_dir`:

```
sandbox path violation: <workspace_path> escapes /home/agentd/projects
```

Workspace paths are absolute and host-specific. If `AGENTD_HOME` or `projects_dir` changed, an
operator must move the existing project directories under the configured root before restarting;
the daemon intentionally rejects stale or outside-root rows rather than relocating them. After
repairing the path, retry via `POST /api/v1/tasks/{id}/retry` and require `COMPLETED` plus a
matching task-ID line in the project's `PLAN_RESULTS.log` as execution evidence.

### If LiteLLM is not running:
- Tasks will fail with LLM errors
- Check logs for "connection refused" or "LLM error"
- Discover the real model alias from the operator’s authenticated `/v1/models` response, then set `LITELLM_BASE_URL`, `LITELLM_API_KEY`, and `LITELLM_MODEL` for agentd.
- Check litellm config mount: `./deploy/docker-plan-execute/litellm/config.yaml:/app/config.yaml:ro`

### If web shows "Not Connected":
- Check if daemon is running on port 8765: `curl -s http://localhost:8765/api/v1/system/status`
- Check browser console for errors
- Verify `NEXT_PUBLIC_API_URL` is `http://localhost:8765` (not `host.docker.internal` - that
  hostname is not defined on Linux and is only needed if fetches happened from inside the
  container, but the web app runs client-side in the browser)
- If web returns 500 with `@tailwindcss/postcss` module error: the container's `NODE_ENV` is
  `production` and `npm install` skipped devDeps. Set `NODE_ENV: "development"` in the compose
  file and restart the web container.

### If tasks don't appear:
- Check daemon logs for errors: `podman logs agentd_agentd_1`
- Verify database has tasks: use `GET /api/v1/projects/{id}/tasks`; the agentd image
  does not ship the `sqlite3` CLI. If direct SQL is required, run a separate utility
  container against the `agentd-data` volume.
- For smoke tests, verify LiteLLM is reachable: `curl -s -H "Authorization: Bearer $LITELLM_API_KEY" http://localhost:4000/v1/models`

### Healthchecks show unhealthy:
- podman-compose 1.3.0 has a known healthcheck quoting bug with exec-form (`CMD`) tests.
  All healthchecks in this compose file use `CMD-SHELL` (string form) to work around it.
- If a service stays unhealthy, run:
  ```bash
  podman logs <service>_<n>
  podman inspect <service>_<n> --format '{{json .State.Health}}'
  ```
- Healthchecks use `depends_on: condition: service_started` (not `service_healthy`) so the
  stack can always start even if a healthcheck is flaky.

### Web container permission issues (EACCES):
- The web container runs as `root` (uid 0) inside the container. With rootless podman, uid 0
  maps to the host user (uid 1000), so host file ownership is preserved and this is safe.
- `npm install` and `next dev` both need write access to the bind-mounted `./web` directory
  (for `node_modules` and `.next` respectively), hence `user: root` is required.

### If chat doesn't trigger plan flow:
- Use action words in the prompt: "build", "create", "implement", "design", "plan", "scrape", etc.
- See the LiteLLM proxy logs for model routing and the agentd logs for plan generation
