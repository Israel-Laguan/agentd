# T-053: Worker Event Timeline and Visible Reasoning Phases

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Parent | none |
| Estimate | L |
| Links | internal/api/sse, internal/queue/worker/worker_events.go |

## Goal

Provide a transparent, structured event timeline for worker reasoning phases, tool calls, and subagent delegation, inspired by Claudinio Code's timeline architecture.

Rather than treating worker execution as an opaque black box with occasional coarse progress updates:
1. Emit granular SSE events for worker thought segments, tool invocations, tool outputs, and phase boundaries.
2. Structure events so external observers (API clients, dashboard, journey tests) can inspect execution flow in real-time or replay from the database event log.
3. Track and emit token counts, latency, and estimated model costs per phase/turn.

## Done when

- [ ] Worker loop emits fine-grained events for thinking blocks, tool dispatches, and subagent lifecycles.
- [ ] SSE endpoints stream the structured execution timeline with correlation IDs.
- [ ] Event log in SQLite preserves worker reasoning timeline for offline inspection and post-mortem debugging.
- [ ] Automated tests verify timeline event emission and ordering under concurrent execution.

## Notes

- Claudinio reference: Nothing is a black box. Renders a timeline with phases, thinking, tool calls, results, subagents, token counts, and cost; sessions persist for inspection.
