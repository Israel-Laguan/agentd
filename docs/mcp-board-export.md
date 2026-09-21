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
| `mcp.http_addr` | `127.0.0.1:0` | Listen address for HTTP transport (loopback only, random port) |

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

For programmatic access. Registered at `/mcp` on the existing API server (same address as `api.address`). Stateless mode — no session tracking.

```bash
curl -X POST http://127.0.0.1:8765/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## Tools

### Read tools

| Tool | Description |
|------|-------------|
| `board.list_tasks` | List tasks, optionally filtered by `project_id` or `state` |
| `board.get_task` | Get a single task with full details and event/comment counts |
| `board.list_projects` | List all projects |
| `board.get_project` | Get a single project |
| `board.list_comments` | List comments on a task |

### Write tools (gated)

| Tool | Description |
|------|-------------|
| `board.add_comment` | Add a comment to a task (author defaults to `WORKER_AGENT`) |
| `board.update_task_state` | Transition a task state (enforces valid transitions) |
| `board.assign_task` | Assign a task to an agent |

All write tools go through the same `KanbanStore` methods as the REST API — state machine validation, optimistic concurrency, and approval boundaries are preserved.

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

2. The agent can now list tasks, read project state, add comments, and transition tasks — all through the MCP protocol while agentd maintains board ownership.

## Debug logging

Set `log_level: debug` or use `-v` to trace MCP tool calls:

```bash
agentd start -v
# or
# config.yaml: log_level: debug
```

Debug logs include `mcp: board.*` entries with task IDs and operation details.
