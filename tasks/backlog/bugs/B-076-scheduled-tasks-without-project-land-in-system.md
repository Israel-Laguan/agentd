# B-076: Scheduled tasks without a project land in _system, which the worker refuses

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Severity | major |
| Links | internal/queue/scheduler.go:195-202 (`resolveProjectID`), internal/queue/worker/worker_support.go:64 (`validateProjectWorkspace`), docs/config-reference.md (agentic `scheduler` row), found by the PR #140 e2e pass (2026-10-19) |

## Symptoms

A scheduled task with no `project_id`, under a scheduler with no `scheduler.project_id`, is dispatched into the `_system` project. The worker claims it (READY → QUEUED), then refuses `_system` ("system project is not an executable workspace") before `MarkTaskRunning`, so the task never runs. `FailHard` cannot fail it either: `UpdateTaskResultState` only moves a RUNNING task, so the task stays QUEUED. With an event sink configured, `FailHard` records an `ERROR` event, but for the failed state update, not the workspace refusal, which is only in the worker log. Dispatch, dedup and `last_fired_at` all look healthy.

## Repro

1. `agentic.scheduler: {enabled: true, tick_interval: 2s, tasks: [{id: s1, run_after: "2020-01-01T00:00:00Z", title: verify-sched}]}`, no `project_id` anywhere.
2. Start the daemon: one task appears in project `00000000-…-0001` (`_system`) about 2 s after boot.
3. The worker logs "invalid project workspace"; the task stays QUEUED, and its only event is an `ERROR` for the failed result update.

## Expected

A schedule either runs, or fails loudly at config load.

## Actual

The task is created in a project that cannot execute it and stays QUEUED. The operator sees an `ERROR` event about a state conflict, not the cause; the cause is in the worker log.

## Notes

- Options, a design call: (a) config-load error when `scheduler.enabled` and a task resolves to no project; (b) a default runnable project; (c) let system-assignee tasks of some types run without a workspace. (a) is the smallest.
- Docs already say to set `project_id` (2026-10-19).
- Check whether the queued-reconcile pass (`queue.queued_reconcile_after`) puts the task back to READY, so the claim, refusal and `ERROR` repeat every interval.
- Then add the scheduler journey proposed by the e2e pass: real `project_id`, 2 s tick, task reaches COMPLETED, exactly once after 2 min.
