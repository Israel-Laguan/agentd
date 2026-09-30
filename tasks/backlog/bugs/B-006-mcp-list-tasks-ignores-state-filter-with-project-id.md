# B-006: board.list_tasks ignores its state filter when project_id is set

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Severity | major |
| Links | internal/mcp/tools_read.go, docs/mcp-board-export.md, test/e2e/journeys_mcp_test.go (J15) |

## Symptoms

`board.list_tasks` accepts both `project_id` and `state`, and
`docs/mcp-board-export.md` advertises the tool as "optionally filtered by
`project_id` **or** `state`". Passing both silently ignores the `state`: the
tool returns every task in the project regardless of the state asked for. A
client polling for "the failed tasks in this project" gets the whole project
and has to filter client-side, with no error to tell it the filter was dropped.

## Repro

Take a project whose tasks are all `COMPLETED` and ask for `state=FAILED`:

```bash
PID=<a project id>
curl -s "http://127.0.0.1:8765/api/v1/projects/$PID/tasks?state=FAILED" | jq '.data|length'
# null (no tasks) — the REST route honours the filter

curl -s -X POST http://127.0.0.1:8765/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"board.list_tasks\",\"arguments\":{\"project_id\":\"$PID\",\"state\":\"FAILED\"}}}"
# returns both COMPLETED tasks
```

Verified live on 2026-09-30. The same call with `state` alone and no
`project_id` filters correctly (returned 0), so it is specifically the
combination that is broken.

## Expected

Either honour both filters, or reject the combination with a clear error
rather than dropping one argument.

## Actual

```go
// internal/mcp/tools_read.go
if in.ProjectID != "" {
    tasks, err = s.store.ListTasksByProject(ctx, in.ProjectID)   // state never applied
    ...
} else if s.board != nil {
    filter := models.TaskFilter{}
    if in.State != "" {
        filter.States = []models.TaskState{models.TaskState(in.State)}
    }
    ...
}
```

The `project_id` branch returns before the filter is ever built.

## Notes

- The input schema declares both properties with neither required, so nothing
  in the tool's own contract says the two are mutually exclusive.
- Same shape of defect as B-005: the `project_id` branch is a shortcut that
  skips the filtering and pagination machinery the board contract already
  provides. Fixing both together — routing the `project_id` case through
  `s.board.ListTasks` with a project filter — would resolve the 100-task
  inconsistency as well.
