# Golden task criteria

Golden criteria give the controller a command to run before accepting a task's
completion. Add `golden_criteria` to a task in a plan, naming an existing
`success_criteria` entry exactly:

```json
{
  "title": "Implement the parser",
  "success_criteria": ["parser tests pass"],
  "golden_criteria": [
    {"criterion": "parser tests pass", "check_command": "go test ./parser/..."}
  ]
}
```

An existing task's criteria can be changed through `PATCH /api/v1/tasks/{id}`
using `success_criteria` and/or `golden_criteria`. Send these fields separately
from state or description changes. A running task cannot be edited; changing
criteria clears its earlier golden-check history. Task split requests accept the
same fields for each subtask.

The controller runs all check commands in the project's workspace through the
configured sandbox executor. A zero exit status passes. The configured
`sandbox.backend` determines isolation: `auto` uses bubblewrap when available
and otherwise falls back to filter-only execution. Checks share the workspace
with the agent, so commands and the tests they invoke should be chosen with that
in mind.

On failure, the task returns to `READY` and the next attempt receives the check
output. `queue.golden_cycle_cap` (default 5) limits failed cycles;
`queue.golden_stall_cap` (default 2) limits consecutive cycles that do not
increase the highest number of passing checks. Reaching either cap sends the
task to `FAILED_REQUIRES_HUMAN`. Approved drafts are checked again before
finalization, since the workspace may have changed during review.
