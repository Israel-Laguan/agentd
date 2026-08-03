# SSE events & tool-event payloads

agentd streams real-time updates over Server-Sent Events at
`GET /api/v1/events/stream` (see [api-testing.md](api-testing.md#event-endpoints)
for the endpoint). This page documents the event-name mapping and the stable
payload contract for tool events — the live agentic-loop activity stream.

Every SSE frame carries a named `event:` line and a `data:` line whose value is a
JSON envelope: `{"topic":"task:<id>","type":"<EVENT_TYPE>","payload":"<JSON string>"}`.
The `event:` name is the SSE-friendly alias of `type`.

## Event-name mapping

agentd emits a fixed set of event types (`internal/models/enums.go`); each is
mapped to an SSE event name in `internal/api/sse/stream.go`:

| Event type | SSE event name | Emitted by | Payload |
| --- | --- | --- | --- |
| `TOOL_CALL` | `tool_called` | `emitToolCall` (`worker_events.go`) + AuditHook (`hooks_builtin_audit.go`) | `ToolCallEvent` |
| `TOOL_RESULT` | `tool_result` | `emitToolResult` (`worker_events.go`) + AuditHook | `ToolResultEvent` |

Other event types (`TASK_*`, `AGENT_*`, `MEMORY_*`, …) follow the same
`type` → SSE-name mapping in `stream.go`. When adding a new event type, extend
the map there and add the matching alias to any consumer (e.g.
`web/lib/sse-events.ts`).

## Tool-event payload contract

The two tool-event payloads are a stable contract between the daemon and any SSE
consumer (the web cockpit, or external integrations). Field names are
`snake_case`. Both carry scrubbed, truncated payloads — **raw arguments/output
never leave the daemon**: the scrubber runs before persistence and the summary
fields are length-capped (`internal/queue/worker/worker_events.go`).

`tool_called` — `ToolCallEvent`:

| Field | Type | Notes |
| --- | --- | --- |
| `tool_name` | string | Name of the invoked tool |
| `call_id` | string | Stable call id; pairs with the later `tool_result` |
| `arguments_summary` | string | Scrubbed tool arguments, truncated to ≤200 chars (`maxArgumentsSummaryLength`); a truncated value ends with `...[truncated]` |

`tool_result` — `ToolResultEvent`:

| Field | Type | Notes |
| --- | --- | --- |
| `tool_name` | string | Name of the tool that produced the result |
| `call_id` | string | Matches the originating `tool_called` call id |
| `exit_code` | int | Conventional exit code (`0` = success; `-1` for error/timeout/vetoed/fatal) |
| `duration_ms` | int64 | Tool execution wall-clock duration in milliseconds |
| `output_summary` | string | Scrubbed tool output, truncated to ≤1000 chars (`maxOutputSummaryLength`); truncation suffix `...[truncated]` |
| `stdout_bytes` | int | Length of captured stdout |
| `stderr_bytes` | int | Length of captured stderr |

Consumers pair a `tool_result` to its earlier `tool_called` by shared `call_id`
(where ordering allows); results whose call was not seen, and calls that have not
yet returned, render standalone.