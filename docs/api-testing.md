# agentd API Testing Reference

This document provides API reference documentation for the agentd HTTP endpoints, derived from integration and unit tests.

## API Overview

The agentd API follows a consistent JSON envelope format:

```json
{
  "status": "success",
  "data": { ... },
  "meta": { "page": 1, "per_page": 10, "total": 100 },
  "error": { "code": "NOT_FOUND", "message": "..." }
}
```

- `status`: `"success"` or `"error"`
- `data`: Response payload (varies by endpoint)
- `meta`: Pagination metadata for list endpoints
- `error`: Error details (present when status is "error")

**Base URL**: `http://localhost:8765`

### Write guard

A web page you open can make your browser send requests to the daemon, so every request passes a guard first:

- **`Host`** must be `localhost`, a loopback IP, or an entry of `api.allowed_hosts`. Anything else gets `403 FORBIDDEN` (this stops DNS rebinding).
- **Writes** (`POST`, `PUT`, `PATCH`, `DELETE`) that carry an `Origin` header must come from `api.ui_origin` (default `http://localhost:3000`). Other origins get `403 FORBIDDEN`. Clients that are not browsers send no `Origin` and are not affected. Requests from the UI origin do not need the materialize token; a page on another site cannot forge its `Origin`.
- **Writes with a body** must send `Content-Type: application/json`, otherwise `415`. With curl, add `-H 'Content-Type: application/json'` (`-d` alone sends a form type). Writes without a body, such as `workspace/ready` and `breaker/reset`, need no content type.

---

## System Endpoints

### GET /api/v1/system/status

Returns system health, memory usage, circuit breaker state, and task summary.

**Query Parameters:** `include_healing`, `include_system` (default false; set `true` to count self-healing handoff subtasks or include `_system`).

**Response**:

```json
{
  "status": "success",
  "data": {
    "status": {
      "kind": "status_report",
      "message": "No active projects. Send a plan request to get started.",
      "summary": {
        "total_projects": 0,
        "tasks_by_state": {}
      }
    },
    "breaker": { "state": "CLOSED", "failure_count": 0, "open_for": 0 },
    "memory": { "heap_alloc": 1085376, "heap_sys": 7634944, "num_gc": 0 },
    "built_at": "2026-05-09T16:44:58.893133749Z"
  }
}
```

`breaker` is the global dispatch breaker; `provider_breakers` (present once a provider has a breaker) maps each provider name to the same fields:

| Field | Meaning |
|-------|---------|
| `state` | `CLOSED` (dispatching), `OPEN` (dispatch stopped) or `HALF_OPEN` (one probe allowed) |
| `failure_count` | Consecutive failures counted toward tripping |
| `open_for` | Time since the breaker opened, in **nanoseconds** (a Go `time.Duration`); `0` unless `OPEN` |
| `last_error` | Last recorded failure; left out when there is none |

The web UI shows any breaker that is not `CLOSED` in the header, and the footer reads `PAUSED: BREAKER OPEN` while the global one is open.

**Test Coverage**: `internal/api/tests/feature/api_http_test.go` (`TestSystemStatus`)

### POST /api/v1/system/breaker/reset

Reset the LLM circuit breaker without restarting the daemon. Use this after quota exhaustion or a transient provider outage has tripped the breaker and you want to resume task execution.

**Query Parameters** (optional):

| Parameter | Description |
|-----------|-------------|
| `provider` | Reset only the named provider's breaker (e.g. `?provider=gemini`). Omit to reset the global breaker and all per-provider breakers. |

**Response**:

```json
{
  "status": "success",
  "data": { "reset": true, "provider": "" }
}
```

When `?provider=gemini` is supplied, `"provider"` in the response is `"gemini"`.

**Error Responses** (all `503`, code `UNAVAILABLE`): system service not configured; global reset unavailable; per-provider reset unavailable when `ProviderBreakers` is nil. An unknown `?provider=` (no breaker recorded for that name yet) is `404 NOT_FOUND`.

**Test Coverage**:

- No service (503): `internal/api/controllers/system_test.go:42`
- No resetter (503): `internal/api/controllers/system_test.go:52`
- Global reset success (200): `internal/api/controllers/system_test.go:66`
- Per-provider reset success (200): `internal/api/controllers/system_test.go:96`
- Per-provider reset, no breakers (503): `internal/api/controllers/system_test.go:114`

---

## Report Endpoints

### GET /api/v1/reports/ai-usage

Aggregates the stored AI call records (one per provider attempt) into rows by the
chosen dimensions. What a record is, the purposes and how to read the columns:
[ai-usage-report.md](ai-usage-report.md). The CLI equivalent is `agentd report ai`.

**Query Parameters** (all optional):

| Parameter | Meaning |
|-----------|---------|
| `group_by` | Comma list of `purpose`, `stage`, `model`, `day`, `task`, `request` (default `purpose`) |
| `from`, `to` | `YYYY-MM-DD` or RFC3339. `from` is inclusive, `to` exclusive; a bare date for `to` includes that whole day |
| `task_id` | Only this task |
| `request` | Only this chat request (its correlation ID) |
| `purpose`, `stage`, `model` | Exact-match filters |

**Response**:

```json
{
  "status": "success",
  "data": {
    "group_by": ["purpose"],
    "rows": [{
      "group": { "purpose": "exec.turn" }, "calls": 3, "failed": 1, "tokens": 350,
      "avg_latency_ms": 166.7, "max_latency_ms": 300, "repair_rate": 0.5, "cascade_rate": 0.333
    }],
    "totals": { "calls": 3, "tokens": 350 },
    "dropped_records": 0
  }
}
```

`repair_rate` is `json_repair` attempts over first attempts; `cascade_rate` is
cascade attempts over all attempts. `dropped_records` counts records the writer
discarded since the daemon started.

**Errors**: `400 VALIDATION_FAILED` for an unknown `group_by`, an unparsable date
or `to` not after `from`; `503 UNAVAILABLE` when the daemon's store keeps no
`ai_calls`.

Rows also carry `mix_chars` (input chars per prompt section), `truncated_calls`
and `cut_by` (calls cut per layer); see "What the context is made of" in the
report doc. `agentd report ai --mix` prints them.

**Test Coverage**: `internal/api/controllers/reports_test.go`, `cmd/agentd/ai_usage_wiring_test.go`

---

### GET /api/v1/reports/tool-use

Tool calls by tool, and the files a task read more than once. Query parameters
(all optional): `task_id`, `from`, `to` (same rules as above). The response
`data` has `tools` (`tool`, `calls`, `failed`, `result_bytes`), `reads`,
`read_bytes`, `reread_bytes` and `repeated_reads` (`task_id`, `path`, `reads`,
`bytes`, `reread_bytes`, top 20). `503 UNAVAILABLE` when the store keeps no tool
calls. The CLI equivalent is `agentd report tools`.

**Test Coverage**: `internal/api/server/tool_use_route_test.go`, `cmd/agentd/report_tools_test.go`

---

## Project Endpoints

### GET /api/v1/projects

List all projects.

**Query Parameters:** `include_system` (default false).

**Response**:

```json
{
  "status": "success",
  "data": null,
  "meta": { "page": 1, "per_page": 0, "total": 0 }
}
```

**Test Coverage**: `internal/api/tests/feature/api_http_test.go` (`TestProjectsList`)

### POST /api/v1/projects/materialize

Create a project from a draft plan.

**Request Body**:

```json
{
  "project_name": "project-name",
  "source_path": "/path/to/local/repo",
  "tasks": [
    { "title": "Task title", "description": "...", "agent_id": "researcher" }
  ]
}
```

Optional top-level `source_path` — local directory whose contents are copied into the project workspace before tasks become claimable. When set, root tasks are `READY` in the response (workspace already populated). When omitted, root tasks start `PENDING` until `POST /api/v1/projects/{id}/workspace/ready` is called. See [`workspace-seeding.md`](workspace-seeding.md).

Optional per-task `agent_id` pre-assigns an agent at creation (default: `default`). Unknown IDs → `404 NOT_FOUND`.

When `api.materialize_token` is set in daemon config, include header `X-Agentd-Materialize-Token: <token>` on this request.

**Response**: Returns the created project and its tasks.

**Test Coverage**: `internal/api/controllers/projects_test.go`, `internal/services/workspace_seeding_test.go`

### POST /api/v1/projects/{id}/workspace/ready

Signals that the project workspace has been populated manually. Transitions `PENDING` root tasks to `READY`.

**Behavior:**

- Validates the workspace directory is non-empty before unlocking.
- Returns `409 STATE_CONFLICT` if the workspace is still empty.
- When `api.materialize_token` is configured, requires the same `X-Agentd-Materialize-Token` header as materialize.

**Response**: Returns the updated task list.

**Test Coverage**: `internal/api/controllers/projects_test.go`

---

## Task Endpoints

Task listing, state updates, comments, assignment, splitting, and retry are
documented in [Task API reference](api-tasks.md).

---

## Agent Endpoints

### GET /api/v1/agents

List all agent profiles.

**Response**:

```json
{
  "status": "success",
  "data": [
    {
      "id": "default",
      "name": "Default Coding Agent",
      "provider": "openai",
      "model": "gpt-4o",
      "temperature": 0.2,
      "system_prompt": "Suggest one safe shell command...",
      "role": "CODE_GEN",
      "max_tokens": 1024,
      "agentic_mode": true
    }
  ]
}
```

`agentic_mode` (boolean, default `true`). The API accepts only `true`: `false` is refused with HTTP 400 (`agentic_mode false is not supported`), because every task runs the inner agentic worker loop with tool round-tripping. A profile whose provider cannot call tools is also rejected with HTTP 400 (`agentic profile's provider cannot call tools`) on create and patch, and a task already on such a profile fails.

`allowed_tools` (string array, default empty) limits the profile to the named tools. A call to any other tool is vetoed at dispatch, delegation included. Empty means no allowlist. `workspace_readonly` (boolean, default `false`) is stored now; nothing enforces it yet.

**Test Coverage**: `internal/api/tests/feature/api_http_test.go` (`TestAgentsList`)

### GET /api/v1/agents/{id}

Get a specific agent profile.

**Test Coverage**:

- `default` agent: `internal/api/tests/feature/api_http_test.go` (`TestAgentsDefault`)
- `qa` agent: `internal/api/tests/feature/api_http_test.go` (`TestAgentsQA`)
- `researcher` agent: `internal/api/tests/feature/api_http_test.go` (`TestAgentsResearcher`)

### POST /api/v1/agents

Create an agent profile. Optional `agentic_mode` (boolean, default `true`; `false` is refused with HTTP 400), `allowed_tools` and `workspace_readonly`. Omit `provider` and `model` (or set both to empty strings) to delegate routing to `gateway.order`. Only the seeded `default` profile follows a matching `stages:` route; other profile IDs bypass `stages:` routes and use the gateway cascade, even when provider and model are empty, matching the built-in seeded profiles from `agentd init`.

### PATCH /api/v1/agents/{id}

Sparse update. `agentic_mode` set to `false` is refused with HTTP 400, as on create. Setting it to `true` on a provider that cannot call tools is rejected. `allowed_tools` and `workspace_readonly` can also be patched.

---

## Chat Endpoints

### POST /v1/chat/completions

OpenAI-compatible chat completions endpoint.

**Request Body**:

```json
{
  "model": "agentd",
  "messages": [
    { "role": "user", "content": "What is the status of the current project?" }
  ],
  "stream": false
}
```

**Response**:

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1778345105,
  "model": "agentd",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "{\"kind\":\"status_report\",\"message\":\"...\"}"
      },
      "finish_reason": "stop"
    }
  ]
}
```

**Test Coverage**: `internal/api/tests/feature/api_http_test.go` (`TestChatCompletion`)

---

## Event Endpoints

### GET /api/v1/events/stream

Server-Sent Events (SSE) stream for real-time updates.

**Query Parameters**:

- `task_id` - Filter events by task
- `project_id` - Filter events by project

**Note**: Events are only emitted during active daemon processing. When idle, no events are streamed.

For the event-name mapping and the `tool_called` / `tool_result` payload contract, see [SSE events & tool-event payloads](sse-events.md).

---

## Error Codes

| Code | HTTP Status | Description |
| ------ | ------------- | ------------- |
| `NOT_FOUND` | 404 | Resource not found |
| `BAD_REQUEST` | 400 | Malformed request (unreadable or invalid JSON body) |
| `VALIDATION_FAILED` | 400 | Invalid request parameters, including a task state that does not exist or a bad AI usage report query |
| `FORBIDDEN` | 403 | Sandbox path violation |
| `NOT_FOUND` | 404 | Resource not found, including a breaker `provider` with no breaker |
| `STATE_CONFLICT` | 409 | A task transition the rules forbid, or a stale write |
| `INTERNAL_ERROR` | 500 | Server error |
| `UNAVAILABLE` | 503 | A daemon component is not configured (system service, task store, human handoff, event bus, AI usage records) |

Every error, including the `GET /events/stream` 503, uses the `{status: "error", error: {code, message}}` envelope. Controllers take the code from an `httpx.Code*` constant; a test pins that. Chat streaming (`stream: true`) reports a planner failure as a chunk of assistant text, as the OpenAI wire format has no error frame.

---

## Task States

See [`reference.md` — Task States](reference.md#task-states).

---

## Running the Tests

```sh
make test PKG=./internal/api/...
make test PKG=./internal/api/...
make check
```

Toolchain troubleshooting: [`REVIEW.md`](../REVIEW.md#go-toolchain-troubleshooting).
