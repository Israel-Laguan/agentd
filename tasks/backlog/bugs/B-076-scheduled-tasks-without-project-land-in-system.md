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

A scheduled task with no `project_id`, under a scheduler with no `scheduler.project_id`, is dispatched into the `_system` project. The worker refuses `_system` ("system project is not an executable workspace"), so the task never runs. Dispatch, dedup and `last_fired_at` all look healthy, so nothing tells the operator.

## Repro

1. `agentic.scheduler: {enabled: true, tick_interval: 2s, tasks: [{id: s1, run_after: "2020-01-01T00:00:00Z", title: verify-sched}]}`, no `project_id` anywhere.
2. Start the daemon: one task appears in project `00000000-…-0001` (`_system`) about 2 s after boot.
3. The worker logs the sandbox violation; the task stays queued.

## Expected

A schedule either runs, or fails loudly at config load.

## Actual

The task is created in a project that cannot execute it, with only a worker log line.

## Notes

- Options, a design call: (a) config-load error when `scheduler.enabled` and a task resolves to no project; (b) a default runnable project; (c) let system-assignee tasks of some types run without a workspace. (a) is the smallest.
- Docs already say to set `project_id` (2026-10-19).
- Then add the scheduler journey proposed by the e2e pass: real `project_id`, 2 s tick, task reaches COMPLETED, exactly once after 2 min.
