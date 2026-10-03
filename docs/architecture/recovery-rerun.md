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
- **The same workspace, as the killed attempt left it, unless the opt-in reset is on.** The workspace is per project (`<projects_dir>/<project id>`), not per task. By default no code cleans it between attempts, so a file the first attempt wrote is still there (`internal/sandbox/rerun_idempotency_spike_test.go` pins this). With `recovery.clean_workspace_on_recover` on, boot reconcile may empty it first; see below.
- **Its own agentd state.** The task row restarts from READY, so counters in SQLite cannot double-count. Only the files on disk carry over.

## What that means

- A command that appends or otherwise depends on a clean directory can produce a different result on the second attempt. The effect lands in the workspace file, not in agentd's state, which is why recovery is judged safe rather than unsafe.
- The re-run does run to completion. J08 kills the daemon mid-task, restarts it on the same volume, and asserts the task reaches COMPLETED with the command's final output in its RESULT, not only that a `RECOVERY` event appears.

## Opt-in workspace reset

`recovery.clean_workspace_on_recover` (default `false`) resets a recovered task's workspace to the empty directory its project started as, so the re-run does not inherit the interrupted attempt's files. It is deliberately narrow:

- **Boot reconcile only.** It runs before any worker starts, so nothing can be writing to the workspace. Tasks recovered by the stale-heartbeat sweep keep the default behaviour, because that sweep runs beside live dispatch and a reset could race the re-dispatched attempt.
- **Only for projects that started empty.** The restorable baseline is a persisted bit, `projects.started_empty` (schema v20): true when the project was materialized with `start_empty_workspace` and no `source_path`. A `source_path` seed or a hand-populated workspace is not recorded, so there is nothing to restore, and every project that predates v20 reads as false. Recording `source_path` instead would restore whatever the directory holds *now*, not the initial state, and would still not cover hand-populated projects.
- **Guarded.** Before resetting, boot reconcile refuses if the project has any other COMPLETED or RUNNING task (a reset would delete its output). A refusal, or a failed reset, fails the recovered task as FAILED_REQUIRES_HUMAN and emits `RECOVERY_RESET_REFUSED` with the reason. It never re-runs the task on a workspace the operator asked to be clean but that is not. A successful reset emits `RECOVERY_WORKSPACE_RESET`.
- **Jailed like `SecureDelete`.** `FSWorkspaceManager.ResetProjectDir` resolves the path under the projects root, refuses the root and any path that escapes it, and empties the project directory while keeping the directory itself so the persisted workspace path stays valid.
- **Once per project.** Several recovered tasks of one project share a single reset.

The "started empty" bit means empty when the project was created. Files an operator drops into a start-empty project's directory afterwards are not tracked and would be deleted by the reset.

## Known limits

- **Without the flag there is no per-attempt workspace reset**, and with it only boot-recovered tasks of start-empty projects are reset.
- **A surviving first attempt can overlap.** Recovery does not verify or terminate the killed attempt's process group. A command that outlives its daemon can still be running while the re-run starts in the same workspace.
