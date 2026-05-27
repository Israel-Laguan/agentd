# Workspace Seeding Guide

This document describes how to reliably populate a project workspace **before**
any worker claims and executes tasks. Three complementary mechanisms are
provided to eliminate the race between workspace seeding and task dispatch.

---

## Problem

`POST /api/v1/projects/materialize` creates an empty workspace at
`~/.agentd/projects/<project-id>/`. Without explicit seeding, workers may
dispatch against an empty directory, causing task failures or sandbox
violations.

---

## Recommended Workflow: Option A — `source_path` on Materialize

The simplest and safest approach. Include a `source_path` field pointing to a
local directory whose contents will be **atomically copied** into the workspace
before any tasks become claimable:

```bash
curl -X POST http://127.0.0.1:8765/api/v1/projects/materialize \
  -H 'Content-Type: application/json' \
  -d '{
    "project_name": "my-project",
    "source_path": "/path/to/local/repo",
    "tasks": [
      {"temp_id": "t1", "title": "Build", "description": "Run make build", "assignee": "SYSTEM"}
    ]
  }'
```

**Behavior:**
- The server creates the project and workspace directory.
- All files from `source_path` are recursively copied into the workspace.
- Only after the copy completes are tasks transitioned from `PENDING` to `READY`.
- Workers cannot claim the tasks until the copy finishes.

**Guarantees:**
- Tasks are `READY` in the response — workspace is already populated.
- No race window: copy is synchronous within the HTTP request.

---

## Option B — Explicit `workspace/ready` Endpoint

When `source_path` is **not** provided, tasks are created in `PENDING` state.
Workers will **not** claim `PENDING` tasks. After seeding the workspace
externally (e.g., via rsync, git clone, file copy), signal readiness:

```bash
# 1. Materialize (tasks start PENDING)
RESP=$(curl -s -X POST http://127.0.0.1:8765/api/v1/projects/materialize \
  -H 'Content-Type: application/json' \
  -d '{"project_name":"my-project","tasks":[...]}')
PROJECT_ID=$(echo "$RESP" | jq -r '.data.project.ID')

# 2. Seed workspace externally
rsync -a ./my-repo/ ~/.agentd/projects/$PROJECT_ID/

# 3. Signal workspace is ready — unlocks tasks to READY
curl -X POST http://127.0.0.1:8765/api/v1/projects/$PROJECT_ID/workspace/ready
```

**Behavior:**
- `POST /api/v1/projects/{id}/workspace/ready` checks that the workspace
  directory is non-empty.
- If empty, returns `409 Conflict` with error message.
- If populated, transitions all `PENDING` tasks to `READY`.

---

## Option C — Worker-Level Safety Net

As an additional safety measure, the worker emits a `WARNING` event if it
encounters an empty workspace directory at dispatch time. This helps detect
misconfigured pipelines where neither Option A nor Option B was used.

The warning event has type `WARNING` and payload:
```
workspace empty at dispatch time; consider using source_path on materialize or calling POST /workspace/ready after seeding
```

This warning does **not** block execution — it serves as an observable signal
for monitoring.

---

## Task State Machine

```
           source_path set              source_path empty
               │                              │
               ▼                              ▼
     ┌──────────────────┐          ┌──────────────────┐
     │  PENDING (brief) │          │     PENDING      │
     └────────┬─────────┘          └────────┬─────────┘
              │                              │
     copy completes                  workspace/ready call
              │                              │
              ▼                              ▼
     ┌──────────────────┐          ┌──────────────────┐
     │      READY       │          │      READY       │
     └────────┬─────────┘          └────────┬─────────┘
              │                              │
        worker claims                  worker claims
              │                              │
              ▼                              ▼
     ┌──────────────────┐          ┌──────────────────┐
     │     QUEUED       │          │     QUEUED       │
     └──────────────────┘          └──────────────────┘
```

---

## API Reference

### POST /api/v1/projects/materialize

Extended request body:

| Field          | Type     | Required | Description |
|----------------|----------|----------|-------------|
| `project_name` | string   | yes      | Project display name |
| `description`  | string   | no       | Project description |
| `source_path`  | string   | no       | Local directory to copy into workspace before tasks become claimable |
| `tasks`        | array    | yes      | Array of task drafts |

### POST /api/v1/projects/{id}/workspace/ready

Signals that workspace content has been seeded externally. Transitions all
`PENDING` tasks for the project to `READY`.

**Responses:**
- `200 OK` — Tasks unlocked. Body: `{"data": {"tasks": [...]}}`
- `409 Conflict` — Workspace directory is empty.

---

## Best Practices

1. **Prefer `source_path`** for deterministic, race-free seeding.
2. Use the `workspace/ready` endpoint for large repos where rsync/git-clone
   takes significant time and you want progress visibility.
3. Monitor for `WARNING` events to detect pipelines that bypass both options.
4. Always use **relative paths** in task descriptions to avoid sandbox
   violations — the LLM's working directory is the workspace root.
