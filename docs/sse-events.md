# SSE events & tool-event payloads

agentd streams real-time updates over Server-Sent Events at
`GET /api/v1/events/stream` (see [api-testing.md](api-testing.md#event-endpoints)
for the endpoint). This page documents the event-name mapping and the stable
payload contract for tool events — the live agentic-loop activity stream.

> ⚠️ **Trusted-local only.** The events stream currently has no authentication,
> authorization, or tenant-ownership checks before subscriptions are created.
> Deploy behind a trusted-local network (or a reverse proxy enforcing access
> control) until those checks are added. Do not expose the endpoint directly to
> untrusted networks.

Every SSE frame carries a named `event:` line and a `data:` line whose value is a
JSON envelope: `{"topic":"task:<id>","type":"<EVENT_TYPE>","payload":"<JSON string>"}`.
The `event:` name is the SSE-friendly alias of `type`.

## Event-name mapping

agentd's persisted, enum-backed event types live in `internal/models/enums.go`,
but workers and bus bridges also publish additional arbitrary string types, so
the live inventory is a superset of that enum. The SSE `event:` name is the
SSE-friendly alias (`eventName`) of the Kafka/bus `type`, which is lowercased;
unknown types fall back to their lowercased type string. Consumers should match
on the `event:` name and safely ignore unrecognized frames. Known tool-event
types map as follows in `internal/api/sse/stream.go`:

| Event type | SSE event name | Emitted by | Payload |
| --- | --- | --- | --- |
| `TOOL_CALL` | `tool_called` | AuditHook (`internal/agent/hooks/hooks_builtin_audit.go`) | `ToolCallEvent` |
| `TOOL_RESULT` | `tool_result` | AuditHook (`internal/agent/hooks/hooks_builtin_audit.go`) | `ToolResultEvent` |

The live agentic dispatch path publishes these two via the AuditHook; the
`emitToolCall` / `emitToolResult` helpers in `worker_events.go` are primarily
exercised by tests. Other event types (`TASK_*`, `AGENT_*`, `MEMORY_*`, …) follow
the same `type` → SSE-name mapping in `stream.go`. When adding a new event type,
extend the map there and add the matching alias to any consumer (e.g.
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
| `exit_code` | int | Conventional exit code (`0` = success; preserves an explicit tool exit code; `-1` when an error/timeout/vetoed/fatal outcome has no explicit exit code) |
| `duration_ms` | int64 | Tool execution wall-clock duration in milliseconds |
| `output_summary` | string | Scrubbed tool output, truncated to ≤1000 chars (`maxOutputSummaryLength`); truncation suffix `...[truncated]` |
| `stdout_bytes` | int | Captured stdout length for structured sandbox results; falls back to result-content length when no stdout envelope is available |
| `stderr_bytes` | int | Length of captured stderr |

Consumers pair a `tool_result` to its earlier `tool_called` by shared `call_id`
(where ordering allows); results whose call was not seen, and calls that have not
yet returned, render standalone.
