# B-030: Plan prompt never states the DraftPlan schema, so `agentd ask` fails with an opaque 500

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done |
| Priority | P1 |
| Sprint | backlog |
| Severity | blocker |
| Links | internal/gateway/routing/router_plan.go (`phasePlanningPrompt`), internal/gateway/correction/correction.go, internal/models/plan.go |

## Symptoms

`agentd ask "<task>"` fails with `draft request failed with status Internal Server Error`. The daemon log shows `chat intake: planner failed ... invalid JSON response`. The model returned valid JSON in the wrong shape, e.g. `{"project":"machine-inspection","report":{...}}` and `{"project_name":..., "tasks":[{"task_name":..., "commands":[...]}]}`.

## Repro

1. LiteLLM → `poolside/laguna-m.1`, `gateway.order: [litellm]`, throwaway `--home`, `agentd start --skip-llm-warmup`.
2. `echo Y | agentd ask "Create a tiny project with one task: run uname -a, lscpu, free -h and lsblk on this machine and write the OS and hardware summary to report.md."`
3. `ask "Inspect this machine (run uname, lscpu, free, lsblk) and write a short report of its OS and hardware."`

Both fail (3 attempts each, `MaxJSONAttempts = 3`).

## Expected

The planner returns a `DraftPlan` (`project_name`, `tasks[].title/description/assignee`), or fails with an actionable message.

## Actual

- `phasePlanningPrompt` is only "Create an agentd DraftPlan. Output ONLY valid JSON. Emit at most N tasks…". It never lists the field names, so the model has to guess them.
- The self-correction message is "Your previous response was invalid JSON. Error: task 1: title is required. Please output ONLY valid JSON." It doesn't show the schema, so retries don't converge.
- The failure reaches the CLI as a bare 500 with no hint, and the cause is visible only in the daemon WARN log.

## Notes

- Fix direction: put the DraftPlan JSON schema plus one example in the system prompt; include the schema in the correction message; map plan-validation errors to a 4xx/422 with the validation text.
- Add a Go test with a fake gateway that returns wrong-shaped JSON first, then asserts the correction prompt contains the schema.
- Found during the first manual end-to-end test (OS/hardware task), 2026-10-13.
