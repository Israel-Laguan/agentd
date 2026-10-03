# Recovered-task re-run contract

When the daemon dies mid-task, the task is re-run from the start. This page says what that re-run sees and what it does not, because "is it safe to re-run a recovered task" is a question the next reader will ask. The measurements are in `docs/testing/results.md` (SP-011 cycle); the behaviour is pinned by tests so it cannot change silently.

## What recovery does

| Path | Trigger | Code |
| --- | --- | --- |
| Boot reconcile | A RUNNING task whose owner PID is not alive. The daemon's own PID counts as dead, because the process that stamped it was killed. | `internal/queue/recovery/recover.go` |
| Stale-heartbeat sweep | A RUNNING task with no heartbeat for `heartbeat.stale_after` (default 2m). | `internal/queue/heartbeat_reconcile.go` |

Both reset the task to READY and emit a `RECOVERY` or `HEARTBEAT_RECONCILE` event. The normal dispatch loop then claims it like any READY task. Boot reconcile runs before any worker starts; the sweep runs concurrently with dispatch.

## What the re-run sees

- **A fresh prompt.** The worker builds it from the task title and description, the agent profile, recalled memory lessons and the requester's saved preferences. When healing is on, the tuner may adjust it by attempt number. Nothing from the earlier attempt's events or output is carried in. The `PreviousAttempts` hook that suggested otherwise was dead code and was removed.
- **The same workspace, as the killed attempt left it.** The workspace is per project (`<projects_dir>/<project id>`), not per task, and no code cleans it between attempts. A file the first attempt wrote is still there. `internal/sandbox/rerun_idempotency_spike_test.go` pins this.
- **Its own agentd state.** The task row restarts from READY, so counters in SQLite cannot double-count. Only the files on disk carry over.

## What that means

- A command that appends or otherwise depends on a clean directory can produce a different result on the second attempt. The effect lands in the workspace file, not in agentd's state, which is why recovery is judged safe rather than unsafe.
- The re-run does run to completion. J08 kills the daemon mid-task, restarts it on the same volume, and asserts the task reaches COMPLETED with the command's final output in its RESULT, not only that a `RECOVERY` event appears.

## Known limits

- **No per-attempt workspace reset.** An opt-in reset (`recovery.clean_workspace_on_recover`) is not implemented. The workspace is shared by all tasks in a project and a project's seed content is not recorded, so a reset cannot restore the starting state and would delete sibling tasks' output. It needs a persisted baseline first.
- **A surviving first attempt can overlap.** Recovery does not verify or terminate the killed attempt's process group. A command that outlives its daemon can still be running while the re-run starts in the same workspace.
