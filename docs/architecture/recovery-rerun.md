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
- **Its own agentd state.** Recovery moves the existing task row back to READY; it leaves retry and token counters, `started_at` and prior events intact, and appends a recovery event. Both the SQLite history and the workspace files persist across attempts — the workspace by design, the row because recovery resets state rather than recreating it.

## What that means

- A command that appends or otherwise depends on a clean directory can produce a different result on the second attempt. Recovery resets agentd's task state, but it does not make a command idempotent: external side effects (an API call, a sent message, a paid request) repeat on the re-run just as a file write does. A task whose command has effects outside its workspace needs its own idempotency safeguard. The workspace write is the case agentd can reason about, which is why recovery is judged safe rather than unsafe.
- The re-run does run to completion. J08 kills the daemon mid-task, restarts it on the same volume, and asserts the task reaches COMPLETED with the command's final output in its RESULT, not only that a `RECOVERY` event appears.

## Opt-in workspace reset

`recovery.clean_workspace_on_recover` (default `false`) resets a recovered task's workspace to the empty directory its project started as, so the re-run does not inherit the interrupted attempt's files. It is deliberately narrow:

- **Boot reconcile only.** It runs before any worker starts, so no new attempt can begin against the workspace — but a command from the killed attempt can survive its daemon and keep writing to it (see Known limits). The reset does not stop that. Tasks recovered by the stale-heartbeat sweep keep the default behaviour, because that sweep runs beside live dispatch and a reset could race the re-dispatched attempt.
- **Only for projects that started empty.** The restorable baseline is a persisted bit, `projects.started_empty` (schema v20): true when the project was materialized with `start_empty_workspace` and no `source_path`. A `source_path` seed is never recorded, so there is nothing to restore, and every project that predates v20 reads as false. Recording `source_path` instead would restore whatever the directory holds *now*, not the initial state, and would still not cover hand-populated projects.
- **Guarded.** Before resetting, boot reconcile refuses unless every other task in the project is untouched — PENDING, READY or QUEUED with no `started_at` — and every just-recovered task is still READY. A sibling that has run owns output in the directory whatever state it moved on to (`started_at` survives every transition), and a recovered task that a second daemon sharing this home has already claimed is a live attempt. A refusal, or a failed reset, fails the recovered task as FAILED_REQUIRES_HUMAN and emits `RECOVERY_RESET_REFUSED` with the reason. It never re-runs the task on a workspace the operator asked to be clean but that is not. If the FAILED_REQUIRES_HUMAN transition itself cannot be persisted, boot aborts rather than continuing with the task still claimable. A successful reset emits `RECOVERY_WORKSPACE_RESET`.
- **Jailed like `SecureDelete`.** `FSWorkspaceManager.ResetProjectDir` resolves the path under the projects root, refuses the root and any path that escapes it, and empties the project directory while keeping the directory itself so the persisted workspace path stays valid. Enumeration and deletion go through an `os.Root` handle on the projects root rather than through resolved paths, so a workspace directory replaced by a symlink mid-reset cannot make the removal escape the root.
- **Once per project.** Several recovered tasks of one project share a single reset.

The "started empty" bit means empty when the project was created. An operator who hand-populates such a workspace and calls `POST /workspace/ready` clears the bit, so that seeded content is preserved; until they do, files they drop into the directory are not tracked and would be deleted by the reset.

**Decision (T-037): document, do not guard further.** Calling `POST /workspace/ready` is the supported way to say "this workspace now holds my content", and it is the guard. A second one (clearing the bit when a non-agent write is detected) would have to tell an operator's file from the interrupted attempt's output on disk, and nothing records which is which: the bit is the only record of the workspace. The reset is opt-in and refuses by default, so the exposure is an operator who enables it and then adds files by hand to a running start-empty project without calling ready. Both sides of the boundary are pinned in `internal/queue/recovery/recover_workspace_hand_added_test.go`: an untracked hand-added file is deleted, and the same file survives (the recovered task is refused loudly) once the bit is cleared. A change to either is a deliberate one.

## Known limits

- **Without the flag there is no per-attempt workspace reset**, and with it only boot-recovered tasks of start-empty projects are reset.
- **A surviving first attempt can overlap.** Recovery does not verify or terminate the killed attempt's process group. A command that outlives its daemon can still be running while the re-run starts in the same workspace — and the workspace reset does not stop it, so its writes can land after the reset.
