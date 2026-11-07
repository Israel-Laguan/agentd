# B-057: `delegate` tool schema does not list the available subagent names

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog (planned in agentd, never executed; augmented into the harness-extraction plan on 2026-11-05 (builtin loop removal itself is the pending extraction, T-071/T-072)) |
| Priority | P3 |
| Sprint | — (agentd S20, conditional, not run) |
| Links | `DelegateToolDefinition` / `DelegateParallelToolDefinition` in `internal/agent/subagent/subagent_types.go`, `subagent definition %q not found` in `internal/agent/subagent/subagent_loader.go` |

## Symptoms

Live run (2026-10-16): the model called `delegate` with `subagent: "bash"` twice. Each call returned `subagent definition "bash" not found`, and the model then ran bash itself. Delegation only worked when the task text named `kernel-runner`, a subagent that exists under `.agentd/subagents/`.

## Cause

The `subagent` parameter's description in the `delegate` tool schema reads only "Name of the subagent definition to use (from .agentd/subagents/)" (`DelegateToolDefinition` in `internal/agent/subagent/subagent_types.go`; the parallel-tasks form in `DelegateParallelToolDefinition` says only "Name of the subagent definition to use"). Neither lists the names that exist, so the model has to guess.

## Suggested fix (not applied)

- List the loaded subagent names in the tool description, or
- Make `subagent` an enum of the loaded names.

Either way the schema must be built from the subagent definitions actually loaded for the session, so it cannot drift from what `delegate` accepts.

## Notes

- Report only. No code change in this bug.
- Found 2026-10-16 alongside an earlier unfiled subagent report (no separate backlog entry retained).
