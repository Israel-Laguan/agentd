# agentd Task API Reference

Task endpoints of the agentd HTTP API, derived from integration and unit tests.
Envelope format, base URL, and error codes are described in
[api-testing.md](api-testing.md).

---

## Task Endpoints

### GET /api/v1/projects/{id}/tasks

List tasks for a project with optional filters.

**Query Parameters**:

- `state` - Filter by state (comma-separated): `PENDING`, `READY`, `QUEUED`, `RUNNING`, `BLOCKED`, `COMPLETED`, `FAILED`, `IN_CONSIDERATION`
- `assignee` - Filter by assignee: `HUMAN`, `SYSTEM`, or agent ID
- `include_healing` - When `true`, include self-healing handoff subtasks. Default: excluded.
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

- Basic listing: `internal/api/tests/feature/routes_test.go:24`
- Unknown project: `internal/api/tests/feature/routes_test.go:43`
- Bad state filter: `internal/api/tests/feature/routes_test.go:52`

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

- Update state: `internal/api/tests/feature/routes_test.go:67`
- Reject unknown state: `internal/api/tests/feature/routes_test.go:79`
- Missing task: `internal/api/tests/feature/routes_test.go:88`

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

- Add comment and pause: `internal/api/tests/feature/routes_test.go:137`
- Invalid content: `internal/api/tests/feature/routes_test.go:119`

### POST /api/v1/tasks/{id}/assign

Assign a task to an agent. Reassignment of a `RUNNING` task returns `409 STATE_CONFLICT`; unknown `agent_id` → `404 NOT_FOUND`. Prefer `agent_id` on materialize when known upfront.

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
