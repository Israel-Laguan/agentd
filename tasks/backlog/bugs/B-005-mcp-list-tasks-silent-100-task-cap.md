# B-005: board.list_tasks silently truncates the board at 100 tasks

| Field | Value |
| --- | --- |
| Type | bug |
| Status | fixed (2026-09-30) |
| Priority | P2 |
| Sprint | S07-e2e-journeys |
| Severity | major |
| Links | internal/mcp/tools_read.go, docs/mcp-board-export.md, test/e2e/journeys_mcp_test.go (J15), docs/testing/journeys.md (J15) |

## Fix (2026-09-30)

`board.list_tasks` now takes explicit `limit` (default 200, the store's max
page) and `offset` arguments and routes both the project-scoped and board-wide
calls through the one paginated, filter-aware store method. The cap is no
longer silent: a client pages with `limit`/`offset`, and the default `limit`
of 200 returns any board of up to 200 tasks in a single call. Covered by
`TestServer_ListTasks_BoardWideExposesAllTasks` (unit) and
`TestJ15_MCPBoardExportLargeBoard` (J15, 150-task board).

## Symptoms

`board.list_tasks` without a `project_id` returns at most 100 tasks and gives
no indication that anything was dropped: no `total`, no cursor, no
`truncated` flag. A client exporting a board with more than 100 tasks gets a
complete-looking, silently incomplete answer. Since it is the board-wide call
it is the one an MCP client uses to "see the board", and since truncation is
invisible the client has no way to detect it or page through it.

## Repro

Materialize a project of 101 tasks (a dependency chain works, so nothing
executes), then compare the MCP board-wide call with the REST route:

```bash
# 101 tasks in the project; the REST route reports its own total
curl -s "http://127.0.0.1:8765/api/v1/projects/$PID/tasks?limit=200" | jq '.data|length, .meta'
# 101, {"page":1,"per_page":200,"total":101}

# the MCP board-wide export
curl -s -X POST http://127.0.0.1:8765/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"board.list_tasks","arguments":{}}}'
# exactly 100 tasks, no total anywhere
```

Verified live on 2026-09-30: the board held 101+ tasks and the board-wide call
returned exactly 100.

## Expected

Either a paginated call (offset/limit with a total, like the REST route's
`meta` block) or an explicit marker that the result is truncated.

## Actual

A hard-coded 100 with no way to ask for more:

```go
// internal/mcp/tools_read.go
filter.Pagination.Limit = 100
result, err := s.board.ListTasks(ctx, filter)
```

## Notes

- Inconsistent with the rest of the tool: the same call *with* a `project_id`
  goes through `ListTasksByProject` and has **no** cap at all — the same board
  returned 101 of 101 tasks that way in the same run. So the cap depends on
  which argument you happen to pass.
- `board.list_comments` and the other list tools are single-entity or
  explicitly small, so this is the only tool where the cap bites.
- The REST task list has its own default page size (25) but *does* report
  `meta.total`, so a client can tell it is looking at a page.
