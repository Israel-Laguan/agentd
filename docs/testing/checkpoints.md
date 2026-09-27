# agentd Test Checkpoints

> Part of the [agentd testing plan](../../TESTING_PLAN.md). Each checkpoint lists the
> steps to run and the evidence to capture.

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

> **Compose note:** `devenv/compose.yaml` starts agentd with `--skip-llm-warmup`,
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
  `devenv/compose.yaml` / agentd config (`gateway.order` and `gateway.providers`).

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

Moved to [Plan Materialization Edge Cases and Plan Refinement](materialization-and-plan-refinement.md).

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
