# B-004: A bad source_path returned 500 and orphaned the project row

| Field | Value |
| --- | --- |
| Type | bug |
| Status | fixed (2026-09-30, in the T-027 P1 journey PR) |
| Priority | P2 |
| Sprint | S07-e2e-journeys |
| Severity | major |
| Links | test/e2e/journeys_materialize_test.go (J05), docs/testing/journeys.md (J05), internal/services/project_service.go, internal/sandbox/workspace.go, docs/testing/results.md ("T-027 cycle: P1 journeys") |

## Symptoms

`POST /api/v1/projects/materialize` with a `source_path` naming a directory
that does not exist — or naming a file rather than a directory — answered
**500 INTERNAL_ERROR** with the wrapped OS error, and left the project and its
task rows **persisted**. The tasks stayed `PENDING` forever: the workspace was
never seeded, so `POST /workspace/ready` answered 409 every time and the
project could never be unlocked or cleaned up. There is no
`DELETE /api/v1/projects/{id}` route, so the orphan was permanent.

The 500 also leaked an absolute host path in the error message
(`internal error: seed workspace from source_path: stat source path: stat
/home/agentd/sources/...: no such file or directory`).

## Repro

```bash
curl -s -X POST http://127.0.0.1:8765/api/v1/projects/materialize \
  -H 'Content-Type: application/json' \
  -d '{"project_name":"bad-source","source_path":"/nope","tasks":[{"title":"t1"}]}'
# 500 {"status":"error","error":{"code":"INTERNAL_ERROR","message":"internal error: seed workspace from source_path: ..."}}

curl -s http://127.0.0.1:8765/api/v1/projects | grep bad-source
# the project row is there
```

## Expected

A missing or non-directory `source_path` is a client error, rejected before
any board state is written: 400 with a stable code, and no project row.

## Actual

500 INTERNAL_ERROR, with the project row and its PENDING task rows already
committed. T-025 had already named this as an open gap; the J05 journey hit it
against the live stack.

## Root cause

`ProjectService.MaterializePlan` did the failure-prone filesystem work after
the write:

1. `store.MaterializePlan` committed the project and task rows in its own
   transaction (`internal/kanban/projects_repo.go:52`)
2. `ensureWorkspace` created the directory
3. `SeedFromPath` stat'ed the source path and failed there
   (`internal/services/project_service.go:55-60`)

Nothing rolled back steps 1-2 on a step-3 failure.

## Fix

`sandbox.ValidateSourcePath` now resolves and stats the path before the store
write, and the service wraps a failure in `models.ErrInvalidDraftPlan` so
`httpx.MapError` answers **400 VALIDATION_FAILED**. `SeedFromPath` reuses the
same helper, so there is one definition of what a usable source path is.

Covered by `TestMaterializeRejectsBadSourcePathBeforePersisting` (both
rejections, and asserts no project row is written) and by J05's
`j05RejectsBadSourcePath` / `j05RejectsSourcePathThatIsAFile` steps.

## Notes

- A mid-copy failure (a `WalkDir` error after some files were written) still
  leaves a half-seeded workspace and a persisted project. That case is not
  covered by this fix — it needs a copy-then-swap, and is rare enough to leave
  for a decision.
- The journey spec's expectation that materialize returns 409 was itself
  wrong and is corrected in `docs/testing/journeys.md`.
