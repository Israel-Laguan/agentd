# B-057: `delegate` tool schema does not list the available subagent names

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog (planned in agentd, never executed; moved out with the harness on 2026-11-05) |
| Priority | P3 |
| Sprint | — (agentd S20, conditional, not run) |
| Links | internal/agent/subagent/subagent_types.go (`delegate` tool schema, line 166; parallel form at line 195), internal/agent/subagent/subagent_loader.go (`subagent definition %q not found`), B-056 |

## Symptoms

Live run (2026-10-16): the model called `delegate` with `subagent: "bash"` twice. Each call returned `subagent definition "bash" not found`, and the model then ran bash itself. Delegation only worked when the task text named `kernel-runner`, a subagent that exists under `.agentd/subagents/`.

## Cause

The `subagent` parameter's description in the `delegate` tool schema reads only "Name of the subagent definition to use (from .agentd/subagents/)" (line 166; the parallel-tasks form at line 195 says only "Name of the subagent definition to use"). Neither lists the names that exist, so the model has to guess.

## Suggested fix (not applied)

- List the loaded subagent names in the tool description, or
- Make `subagent` an enum of the loaded names.

Either way the schema must be built from the subagent definitions actually loaded for the session, so it cannot drift from what `delegate` accepts.

## Notes

- Report only. No code change in this bug.
- Found 2026-10-16 alongside B-056.
