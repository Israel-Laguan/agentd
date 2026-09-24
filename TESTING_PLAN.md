# agentd Testing Plan

This document outlines the testing plan to verify agentd functionality through the web interface.

## Prerequisites

- Podman (or Docker) and Podman Compose installed
- Ports 3000, 4000, 8000, 8765 available

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

Services:
- **mockllm** (port 8000) - Fake OpenAI-compatible LLM
- **litellm** (port 4000) - Proxy routing to mockllm
- **agentd** (port 8765) - Daemon
- **web** (port 3000) - Next.js frontend

## Running the Test Environment (Manual)

### Terminal 1: Start mock LLM
```bash
python3 scripts/mock_llm.py --port 8000
```

### Terminal 2: Start agentd daemon
```bash
cd /home/anthony/code/agentd
LITELLM_API_KEY=test ./bin/agentd start --skip-llm-warmup -v
```

### Terminal 3: Start web frontend
```bash
cd /home/anthony/code/agentd/web
NEXT_PUBLIC_USE_MOCK=false npm run dev
```

### Terminal 4: Access the web interface
Open browser to: **http://localhost:3000**

---

## Test Checkpoints

### Checkpoint 1: Verify Logs Appear When Loop Starts and Kanban is Accessible

**Steps:**
1. Open browser to http://localhost:3000
2. Check if the Kanban board loads
3. Look for logs panel/section
4. Verify the daemon logs are visible

**Expected Results:**
- Kanban board displays with task columns
- Logs show daemon activity
- No console errors

**API verification (automated alternative to visual check):**
```bash
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool
curl -s http://localhost:8765/api/v1/system/status | python3 -m json.tool
curl -s -N http://localhost:8765/api/v1/events/stream
```

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

**API verification:**
```bash
curl -s -X POST http://localhost:8765/v1/chat/completions   -H "Content-Type: application/json"   -d '{"model":"mock/agentd","messages":[{"role":"user","content":"Build a holiday card reminder app"}],"tools":[{"type":"function","function":{"name":"create_plan"}}]}'
```

### Checkpoint 3: Kanban View Reflects Database

**Steps:**
1. Create a task via chat (Checkpoint 2)
2. Approve the plan (in the web UI, this triggers `POST /api/v1/projects/materialize`)
3. Check the Kanban board
4. Verify task appears in correct column

**Expected Results:**
- Tasks created via chat appear in Kanban
- Task states (PENDING, RUNNING, COMPLETED, etc.) are correct
- Data matches what's in SQLite database

**API verification:**
```bash
curl -s http://localhost:8765/api/v1/projects | python3 -m json.tool
curl -s http://localhost:8765/api/v1/projects/<PROJECT_ID>/tasks | python3 -m json.tool
podman exec agentd_agentd_1 sqlite3 /home/agentd/global.db "SELECT id, title, state, project_id FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

> **Note:** The database lives in the named volume `agentd-data` at `/home/agentd/global.db`.

---

### Checkpoint 4: Chat Creates Tasks in Kanban and Logs Populate

**Steps:**
1. Send a request to create multiple tasks via chat
2. Example: "Create tasks for: (1) Write documentation, (2) Fix login bug, (3) Add dark mode"
3. Watch the Kanban board for new tasks
4. Observe the logs panel

**Expected Results:**
- New tasks appear in Kanban PENDING column
- Agent processes tasks (moves to RUNNING)
- Logs show task execution details
- Tasks eventually complete (COMPLETED/FAILED)

**API verification:**
```bash
curl -s -X POST http://localhost:8765/v1/chat/completions   -H "Content-Type: application/json"   -d '{"model":"mock/agentd","messages":[{"role":"user","content":"Create tasks for: (1) Write documentation, (2) Fix login bug, (3) Add dark mode"}],"tools":[{"type":"function","function":{"name":"create_plan"}}]}'

curl -s http://localhost:8765/api/v1/projects/<PROJECT_ID>/tasks | python3 -m json.tool
podman logs agentd_agentd_1 -f 2>&1 | grep -E 'task|execut|complete|RUNNING|COMPLETED'
```

---

## Verification Results (verified 2026-09-23)

All four checkpoints were verified end-to-end against the running stack
(`podman compose -f docker-compose.dev.yml up --build -d`):

| Checkpoint | Result | Evidence |
|---|---|---|
| CP1 – Logs + Kanban accessible | ✅ PASS | `GET /` → HTTP 200 with full dashboard HTML (Chat/Board/Logs nav); `GET /api/v1/projects` → 3 projects; SSE endpoint accepts connections |
| CP2 – Chat interface works | ✅ PASS | `POST /v1/chat/completions` returns a `create_plan` tool call containing a DraftPlan |
| CP3 – Kanban reflects database | ✅ PASS | `POST /api/v1/projects/materialize` → HTTP 201; tasks visible via `GET /api/v1/projects/{id}/tasks` |
| CP4 – Multi-task plan + execution | ✅ PASS | After `workspace/ready`, every executable direct and generated task reached `COMPLETED`; `PLAN_RESULTS.log` in the materialized project workspace contains a task-ID evidence line for each direct and generated task (only the intended `AGENT_PLAN` parent may be `BLOCKED`) |

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

Materialized plans **without** a `source_path` intentionally create tasks in `PENDING` and keep
them unclaimable until an operator signals workspace readiness (see
`ProjectService.MaterializePlan` in `internal/services/project_service.go`). To unlock:

```bash
# 1. Seed at least one file into the project workspace (IsWorkspacePopulated requires >=1 entry)
podman exec agentd_agentd_1 sh -c 'echo "# workspace" > /home/agentd/projects/<PROJECT_ID>/README.md'

# 2. Transition PENDING -> READY (dispatch happens within ~10s after this)
curl -s -X POST http://localhost:8765/api/v1/projects/<PROJECT_ID>/workspace/ready
```

Calling step 2 before step 1 returns **HTTP 409** (`workspace is empty; seed content before
marking ready`). No `X-Agentd-Materialize-Token` header is required unless
`api.materialize_token` is set in the agentd config.

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
- Verify: `curl -s http://localhost:4000/health/liveliness` (should return `"I'm alive!"`)
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
- Verify database has tasks: `podman exec agentd_agentd_1 sqlite3 /home/agentd/global.db "SELECT * FROM tasks;"`
- Check mockllm is reachable through litellm: `curl -s -H "Authorization: Bearer sk-demo-litellm-plan-execute-only" http://localhost:4000/v1/models`

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

### Mock LLM doesn't trigger plan flow:
- The mock LLM classifies intent based on keywords in the user message. To trigger a plan:
  - Use action words: "build", "create", "implement", "design", "plan", "scrape", etc.
  - Example: "Build a holiday card reminder app"
- To trigger task execution (worker command): use "Task: <description>" without plan keywords
- To trigger plan decomposition: include "AGENT_PLAN" in the task title
- See `deploy/docker-plan-execute/mockllm/server.py` for the keyword lists.
