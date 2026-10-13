# B-031: Task phrased as a question ("what os and hardware have this machine") is classified as status_check

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done |
| Priority | P2 |
| Sprint | backlog |
| Severity | major |
| Links | docs/frontdesk.md, internal/gateway/routing (`ClassifyIntent`) |

## Symptoms

`agentd ask "what os and hardware have this machine"` fails with `planner returned status_report instead of a draft plan; refine your prompt or use the API directly`. The same text via `POST /v1/chat/completions` returns `status_report: "No active projects. Send a plan request to get started."`

## Repro

1. Daemon with no projects, LiteLLM → `poolside/laguna-m.1`.
2. `echo Y | agentd ask "what os and hardware have this machine"`

## Expected

A request for the agent to find something out is routed as `plan_request`, or the user gets an `intent_clarification`. It should not be answered with a board status report that is unrelated to the question.

## Actual

The classifier picks `status_check`. The CLI error is a dead end, and the "status report" tells the user nothing about the machine.

## Notes

- Check the intent prompt: `status_check` should be limited to questions about the board and its tasks.
- Add a gherkin case in `internal/api/features/openai_intake.feature` for a "what/which … this machine" style plan request.
- `agentd ask` should print the `message` of a `status_report`/clarification instead of a generic failure.
- Found during the first manual end-to-end test, 2026-10-13.
