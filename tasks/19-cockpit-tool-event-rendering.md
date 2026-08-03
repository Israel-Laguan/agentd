# Milestone 19 — Cockpit tool-event rendering (web, independent)

**Status**: not started · **PR scope**: one web PR · **Depends on**: nothing (backend already
emits the events) · **Relates to**: roadmap task 08 (server half done; UI half missing).

## Goal

Surface `TOOL_CALL` / `TOOL_RESULT` events in agentd's web cockpit activity/task view so operators
can watch the agentic loop as it works. The server already streams these as SSE events with scrubbed
payloads; **zero web consumers exist today**.

## Background / current state (verified)

- Server event types: `models.EventTypeToolCall = "TOOL_CALL"`, `EventTypeToolResult =
  "TOOL_RESULT"` (`internal/models/enums.go:159-160`).
- Emitters: `internal/queue/worker/worker_events.go` (`emitToolCall`, `emitToolResult`) and the
  `hooks_builtin_audit.go` AuditHook — payloads scrubbed + truncated (arguments ≤200 chars, output
  ≤1000 chars).
- SSE mapping: `internal/api/sse/stream.go:112-115` maps `TOOL_CALL`→`tool_called`,
  `TOOL_RESULT`→`tool_result`; covered by `stream_test.go`.
- Web grep: **no matches** for `tool_called` / `tool_result` / `TOOL_CALL` under `web/` — the UI
  never consumes these event types.

## Payload shapes (verify against the emitters when implementing)

- `tool_called`: `{ tool_name, call_id, arguments_summary }`
- `tool_result`: `{ tool_name, call_id, exit_code, duration_ms, output_summary, stdout_bytes,
  stderr_bytes }`

## Scope (in)

1. In the web app, consume the `tool_called` / `tool_result` SSE event types in the task/activity
   stream (locate the existing SSE/event handling in `web/`; follow its event-type switch/add
   pattern).
2. Render each tool event in the activity list: tool name; for results show exit code, duration,
   and the scrubbed output summary with sensible truncation/expansion; pair `tool_result` with its
   `call_id` `tool_called` where ordering allows.
3. Web unit tests for the new rendering components/helpers (mirror the app's existing test setup).

## Scope (out)

- No backend changes (event payloads are already scrubbed/structured; adjust only if a display need
  surfaces and pair with the server owner).
- No changes to the other SSE event types already rendered.

## Files to touch (likely; final set via `web/` search)

- web activity/event stream component(s) + their tests (under `web/components`, `web/lib`, per
  existing conventions)
- Add `tool_called` / `tool_result` to the shared event-type mapping (wherever other SSE event
  types are mapped)

## Verification

- `web` test/lint/build pass (project `web` commands).
- Manual: run the daemon with an agentic task (or a fixture SSE source) and confirm tool events
  render in the cockpit activity view with scrubbed summaries and no raw secrets.
