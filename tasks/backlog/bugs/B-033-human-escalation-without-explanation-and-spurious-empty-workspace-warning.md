# B-033: FAILED_REQUIRES_HUMAN leaves no comment or HUMAN task, and "workspace empty" warns on an intentional empty start

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done |
| Priority | P2 |
| Sprint | backlog |
| Severity | minor |
| Links | internal/queue/worker/worker_support.go (~L270), docs/workspace-seeding.md, docs/demo.md |

## Symptoms

1. After the retry budget is exhausted, the task is `FAILED_REQUIRES_HUMAN`, but `GET /api/v1/tasks/<id>/comments` is `[]` and no HUMAN child task appears (healing was disabled in this test config, so this may only be the `healing.enabled: false` case). An operator has to read the raw events list to find out why.
2. Every dispatch logs and emits a `WARNING` event "workspace empty at dispatch time; consider using source_path…", even though the project was materialized with `start_empty_workspace: true`, which docs/workspace-seeding.md calls an explicit choice.

## Repro

Run the scenario from B-032 with `healing.enabled: false`, then fetch `/tasks/<id>/comments` and `/tasks/<id>/events`.

## Expected

- A terminal failure posts a human-readable comment: last error, retry count, what to do next.
- No empty-workspace warning when the project has `started_empty = true`.

## Actual

See Symptoms.

## Notes

- First confirm whether symptom 1 occurs with healing enabled. If not, document that the board is silent in that mode.
- Found during the first manual end-to-end test, 2026-10-13.
