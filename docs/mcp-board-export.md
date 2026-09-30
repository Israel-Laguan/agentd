# MCP Board Export

agentd exposes its kanban board over the [Model Context Protocol](https://modelcontextprotocol.io/), allowing external agents (Claude Code, Cline, etc.) to act as workers behind agentd's control plane.

## Invariant

**agentd owns the board; external agents are workers.** All write operations are routed through agentd's existing store methods, which enforce the same state machine and approval boundaries as the REST API.

## Configuration

```yaml
mcp:
  enabled: false
  transport: stdio     # "stdio", "http", or "both"
  http_addr: "127.0.0.1:0"
```

| Key | Default | Description |
|-----|---------|-------------|
| `mcp.enabled` | `false` | Enable the MCP board-export server |
| `mcp.transport` | `stdio` | Transport: `stdio` (Claude Code/Cline), `http` (Streamable HTTP), or `both` |
| `mcp.http_addr` | `127.0.0.1:0` | **Not implemented.** The key is parsed but never read; the HTTP transport is served on `api.address`. See the note below. |

## Transports

### stdio

For Claude Code and Cline integration. The server reads from stdin and writes to stdout using newline-delimited JSON.

```json
{
  "mcpServers": {
    "agentd": {
      "command": "agentd",
      "args": ["start"]
    }
  }
}
```

### Streamable HTTP

For programmatic access. Registered at `/mcp` on the existing API server (same address as `api.address`). Stateless mode — no session tracking: no `initialize` handshake is required, and no `mcp-session-id` is issued or checked.

The transport requires `Content-Type: application/json` and an `Accept` header covering **both** `application/json` and `text/event-stream`. `Accept: application/json` on its own is rejected with 400, and so is omitting `Accept` entirely — curl's default `*/*` satisfies it, but a programmatic client that does not set it (Go's `http.Client`, for one) gets a 400.

```bash
curl -X POST http://127.0.0.1:8765/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

The response is an SSE stream (`text/event-stream`) carrying a single
`event: message` frame whose `data:` line is the JSON-RPC response — not a bare
JSON body.

```text
event: message
data: {"jsonrpc":"2.0","id":1,"result":{"tools":[...]}}
```

#### `mcp.http_addr` is not wired up

`mcp.http_addr` is defined in `internal/config/mcp.go` and read into the config
struct, but nothing consumes it: with `transport: http` the endpoint is served
by the API server on `api.address`. There is no separate listener and therefore
no random loopback port. Set `api.address` to control where `/mcp` is reachable,
and treat the HTTP transport as unauthenticated — it sits outside every auth
check, so anything that can reach `api.address` gets full board write access
through `board.add_comment`, `board.update_task_state` and `board.assign_task`.
Loopback binding is currently the only thing limiting that.

## Tools

### Read tools

| Tool | Description |
|------|-------------|
| `board.list_tasks` | List tasks, optionally filtered by `project_id` or `state` (see the two caveats below) |
| `board.get_task` | Get a single task with full details and event/comment counts |
| `board.list_projects` | List all projects |
| `board.get_project` | Get a single project |
| `board.list_comments` | List comments on a task |

Two caveats on `board.list_tasks`, both open bugs:

- **Without `project_id` the list is capped at 100 tasks**, silently — no
  total, no cursor, no truncation flag ([B-005](../../tasks/backlog/bugs/B-005-mcp-list-tasks-silent-100-task-cap.md)).
  With `project_id` there is no cap at all.
- **The `state` filter is ignored when `project_id` is also passed**; only one
  of the two is applied ([B-006](../../tasks/backlog/bugs/B-006-mcp-list-tasks-ignores-state-filter-with-project-id.md)).


### Write tools (gated)

| Tool | Description |
|------|-------------|
| `board.add_comment` | Add a comment to a task (author defaults to `WORKER_AGENT`) |
| `board.update_task_state` | Transition a task state (enforces valid transitions) |
| `board.assign_task` | Assign a task to an agent |

All write tools go through the same `KanbanStore` methods as the REST API — state machine validation, optimistic concurrency, and approval boundaries are preserved.

## Response format

Every tool result is a JSON-RPC `result` whose payload is a JSON **string** in
`content[0].text`, with the same value parsed and repeated as
`structuredContent`. A client has to parse the text block; `structuredContent`
is the already-parsed copy.

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [{ "type": "text", "text": "[\n  {\n    \"id\": \"...\"\n  }\n]" }],
    "structuredContent": [{ "id": "..." }]
  }
}
```

| Tool | Payload (decoded from `content[0].text`) |
|------|------------------------------------------|
| `board.list_projects` | array of `{id, name, workspace_path, status}` |
| `board.get_project` | `{id, name, original_input, workspace_path, status}` |
| `board.list_tasks` | array of `{id, title, state, assignee, project_id, agent_id, depends_on}` |
| `board.get_task` | `{id, title, description, state, assignee, project_id, agent_id, depends_on, retry_count, events, comments}` |
| `board.list_comments` | array of `{id, author, body}` |
| `board.add_comment` | `{status, task_id}` |
| `board.update_task_state` | `{status, task_id, state}` |
| `board.assign_task` | `{status, task_id, assignee}` |

**The export carries no task output.** There is no result, output or
transcript field on any tool: `board.get_task` reports *counts* of events and
comments, not their bodies. An external agent that needs what a task actually
did has to read `GET /api/v1/tasks/{id}/events` (durable log) or subscribe to
the SSE stream. Treat the MCP surface as a board *state* summary.

### Errors

Two distinct failure shapes, and clients need to tell them apart:

- **Protocol error** — the call itself is wrong. A JSON-RPC `error` object,
  e.g. `-32602 invalid params` for an unknown tool name.
- **Tool error** — the call was well-formed but could not be satisfied. A
  normal 200 result with `isError: true` and the reason in
  `content[0].text`, e.g. `error: task not found`.

## Example: Claude Code integration

1. Add to your Claude Code MCP config:

```json
{
  "mcpServers": {
    "agentd": {
      "command": "agentd",
      "args": ["start", "--config", "/path/to/config.yaml"]
    }
  }
}
```

1. The agent can now list tasks, read project state, add comments, and transition tasks — all through the MCP protocol while agentd maintains board ownership.

## Debug logging

Set `log_level: debug` or use `-v` to trace MCP tool calls:

```bash
agentd start -v
# or
# config.yaml: log_level: debug
```

Debug logs include `mcp: board.*` entries with task IDs and operation details.
