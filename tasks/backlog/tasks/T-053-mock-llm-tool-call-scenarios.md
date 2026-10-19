# T-053: Mock LLM scenarios that return tool calls

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | devenv/mockllm/server.py, devenv/mockllm/capture.py, test/e2e/, docs/testing/journeys.md, found by the PR #140 e2e pass (2026-10-19) |

## Goal

The devenv mock LLM never returns OpenAI `tool_calls`, so no e2e journey can reach the agentic tool path: the hook chain (denylist), tool results in the working zone (context truncation), subagent delegation. Add scenarios that answer with configured tool calls, then the journeys they unlock.

## Done when

- [ ] A scenario (tag or header, like the existing ones) answers with `tool_calls` whose name and arguments the journey sets, then a final message on the next turn; covered by `devenv/mockllm/test_server.py`.
- [ ] Journey: an agentic task calls `bash` with `sudo id`; the denylist refusal is recorded and the command did not run.
- [ ] Journey: an agentic task gets a tool result larger than `queue.agentic_context`; it completes, and `GET /requests` shows the working zone cut with the configured `gateway.truncation.strategy`.
- [ ] `make check` and `make test-e2e` pass.

## Notes

- No demo scripts: the proofs are Go journeys under `test/e2e/`.
