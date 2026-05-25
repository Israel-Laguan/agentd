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

---

## System Endpoints

### GET /api/v1/system/status

Returns system health, memory usage, circuit breaker state, and task summary.

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

**Test Coverage**: `e2e/http_test.go:20`

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

**Error Responses**:
- `503 UNAVAILABLE` — System service not configured (`SystemHandler.System` is nil)
- `503 UNAVAILABLE` — Neither global resetter nor per-provider breakers are configured (global reset with no `?provider=`)
- `503 UNAVAILABLE` — Per-provider breakers not configured (`?provider=<name>` supplied but `ProviderBreakers` is nil)

```json
{ "status": "error", "error": { "code": "UNAVAILABLE", "message": "breaker reset not available" } }
```

**Test Coverage**:
- No service (503): `internal/api/controllers/system_test.go:42`
- No resetter (503): `internal/api/controllers/system_test.go:52`
- Global reset success (200): `internal/api/controllers/system_test.go:66`
- Per-provider reset success (200): `internal/api/controllers/system_test.go:96`
- Per-provider reset, no breakers (503): `internal/api/controllers/system_test.go:114`

---

## Project Endpoints

### GET /api/v1/projects

List all projects.

**Response**:
```json
{
  "status": "success",
  "data": null,
  "meta": { "page": 1, "per_page": 0, "total": 0 }
}
```

**Test Coverage**: `e2e/http_test.go:33`

### POST /api/v1/projects/materialize

Create a project from a draft plan.

**Request Body**:
```json
{
  "name": "project-name",
  "tasks": [
    { "title": "Task title", "description": "Task description" }
  ]
}
```

**Response**: Returns the created project and its tasks.

---

## Task Endpoints

### GET /api/v1/projects/{id}/tasks

List tasks for a project with optional filters.

**Query Parameters**:
- `state` - Filter by state (comma-separated): `PENDING`, `READY`, `QUEUED`, `RUNNING`, `BLOCKED`, `COMPLETED`, `FAILED`, `IN_CONSIDERATION`
- `assignee` - Filter by assignee: `HUMAN`, `SYSTEM`, or agent ID
- `limit` - Maximum results (default: 50)
- `offset` - Pagination offset

**Response**:
```json
{
  "status": "success",
  "data": [...],
  "meta": { "page": 1, "per_page": 10, "total": 5 }
}
```

**Test Coverage**:
- Basic listing: `internal/api/routes_test.go:24`
- Unknown project: `internal/api/routes_test.go:43`
- Bad state filter: `internal/api/routes_test.go:52`

### PATCH /api/v1/tasks/{id}

Update a task's state.

**Request Body**:
```json
{ "state": "COMPLETED" }
```

**Valid States**: `PENDING`, `IN_CONSIDERATION`, `RUNNING`, `BLOCKED`, `FAILED`, `COMPLETED`

**Response**:
```json
{
  "status": "success",
  "data": {
    "id": "task-id",
    "state": "COMPLETED",
    ...
  }
}
```

**Error Responses**:
- `404 NOT_FOUND` - Task not found
- `409 STATE_CONFLICT` - Invalid state transition

**Test Coverage**:
- Update state: `internal/api/routes_test.go:67`
- Reject unknown state: `internal/api/routes_test.go:79`
- Missing task: `internal/api/routes_test.go:88`

### POST /api/v1/tasks/{id}/comments

Add a human comment to a task. This pauses the task to `IN_CONSIDERATION` state.

**Request Body**:
```json
{ "content": "Please review this task" }
```

**Response**:
```json
{
  "status": "success",
  "data": { "task_id": "task-id" }
}
```

**Error Responses**:
- `400 BAD_REQUEST` - Empty content (validation failed)

**Test Coverage**:
- Add comment and pause: `internal/api/routes_test.go:137`
- Invalid content: `internal/api/routes_test.go:119`

### POST /api/v1/tasks/{id}/assign

Assign a task to an agent.

**Request Body**:
```json
{ "agent_id": "default" }
```

### POST /api/v1/tasks/{id}/split

Split a task into subtasks.

**Request Body**:
```json
{
  "subtasks": [
    { "title": "Subtask 1", "description": "..." },
    { "title": "Subtask 2", "description": "..." }
  ]
}
```

### POST /api/v1/tasks/{id}/retry

Retry a failed task. Allowed from `FAILED`, `BLOCKED`, or `FAILED_REQUIRES_HUMAN` states.

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
      "provider": "llamacpp",
      "model": "qwen",
      "temperature": 0.2,
      "system_prompt": "Suggest one safe shell command...",
      "role": "CODE_GEN",
      "max_tokens": 1024,
      "agentic_mode": false
    }
  ]
}
```

`agentic_mode` (boolean, default `false`) enables the inner agentic worker loop with tool round-tripping. When `true` and the provider supports agentic mode, tasks use `processAgentic`; otherwise the worker falls back to legacy single-shot JSON execution with a warning log.

**Test Coverage**: `e2e/http_test.go:53`

### GET /api/v1/agents/{id}

Get a specific agent profile.

**Test Coverage**:
- `default` agent: `e2e/http_test.go:70`
- `qa` agent: `e2e/http_test.go:87`
- `researcher` agent: `e2e/http_test.go:104`

### POST /api/v1/agents

Create an agent profile. Optional `agentic_mode` (boolean, default `false`). Omit `provider` and `model` (or set both to empty strings) to delegate routing to `gateway.order` and `gateway.role_models`, matching the built-in seeded profiles from `agentd init`.

### PATCH /api/v1/agents/{id}

Sparse update. Set `agentic_mode` to `true` or `false` to enable or disable agentic worker behavior for that profile.

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

**Test Coverage**: `e2e/http_test.go:121`

---

## Event Endpoints

### GET /api/v1/events/stream

Server-Sent Events (SSE) stream for real-time updates.

**Query Parameters**:
- `task_id` - Filter events by task
- `project_id` - Filter events by project

**Note**: Events are only emitted during active daemon processing. When idle, no events are streamed.

---

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `NOT_FOUND` | 404 | Resource not found |
| `STATE_CONFLICT` | 409 | Invalid state transition |
| `VALIDATION_FAILED` | 400 | Invalid request parameters |
| `INTERNAL` | 500 | Server error |

---

## Task States Reference

| State | Meaning |
|-------|---------|
| `PENDING` | Waiting for prerequisite tasks to complete |
| `READY` | All dependencies met; eligible for worker claim |
| `QUEUED` | Claimed by daemon; waiting for worker slot |
| `RUNNING` | Worker actively executing the task |
| `BLOCKED` | Parent paused until child tasks complete |
| `COMPLETED` | Task finished successfully |
| `FAILED` | Task exhausted retries or was evicted |
| `FAILED_REQUIRES_HUMAN` | Task evicted after max retries; human review required |
| `IN_CONSIDERATION` | Human comment interrupted task; awaiting re-evaluation |

---

## Running the Tests

To verify API functionality:

```bash
# Run e2e tests
go test -v ./e2e/...

# Run API unit tests
go test -v ./internal/api/...
```

**Expected Results**: All tests pass (10/10 for e2e, all API route tests pass).

**Stale cache workaround**: If you see inconsistent or stale test results after switching branches, see [`REVIEW.md`](../REVIEW.md#go-build-cache-troubleshooting). Prefer `make test` or `make check`, which use a workspace-local `.gocache/` automatically.