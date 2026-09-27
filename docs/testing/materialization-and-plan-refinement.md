# Plan Materialization Edge Cases and Plan Refinement

Companion to [agentd Test Checkpoints](checkpoints.md) (Checkpoint 3b).

## Checkpoint 3b: Plan Materialization Edge Cases

`ProjectService.MaterializePlan` (`internal/services/project_service.go`) has
two flows — `source_path` (seed synchronously, tasks unlock to READY) and the
explicit-pending flow, taken when **neither** `source_path` nor
`start_empty_workspace` is set (tasks stay PENDING until
`POST workspace/ready`) — each with validation the happy-path checkpoints above
don't exercise. Note that `start_empty_workspace: true` with an empty
`source_path` does *not* keep tasks PENDING: it takes the seed path and unlocks
root tasks to READY.

**Steps and expected results:**

1. Call `POST /api/v1/projects/{id}/workspace/ready` on a project whose
   workspace has **not** been seeded yet (no file written). Expect an error
   (`ErrWorkspaceNotReady`), not a silent success — tasks must remain PENDING.

2. Materialize a plan with `source_path` pointing at a nonexistent or
   unreadable directory. Expect materialization to return an error. Note that
   project/task rows may already be persisted before the error is returned, so
   the failure is not always fully atomic; inspect `GET /api/v1/projects` after
   the call if atomicity matters.

3. Call `workspace/ready` twice in a row after a valid seed. Expect the
   second call to be a safe no-op (or a clear "already ready" response), not
   a duplicate dispatch of already-READY tasks.

4. Materialize a plan with **no** `source_path` and **no**
   `start_empty_workspace` (the explicit-pending flow). Expect eligible root
   tasks to stay PENDING, and only transition after
   `POST /api/v1/projects/{id}/workspace/ready` is seeded and called.

```bash
PROJECT_ID="replace-with-project-id"
curl -s -X POST "http://localhost:8765/api/v1/projects/${PROJECT_ID}/workspace/ready"   # before seeding — expect error
curl -s -X POST http://localhost:8765/api/v1/projects/materialize -H "Content-Type: application/json" \
  -d '{"source_path":"/nonexistent/path", ...}'   # fill in real plan fields; expect clean failure
```

## Known Gap: Refining an Overly Broad Plan

There is currently **no** refine/narrow-scope mechanism in the code
(`models/plan.go`'s `DraftPlan` has no version or parent-plan linkage, and no
"revise" endpoint exists). Pushing back on a plan in chat produces a brand
new `DraftPlan` from scratch rather than a narrowed revision of the original.

**Steps:**

1. Send a deliberately broad request, e.g. "Build me a full SaaS product."

2. Push back in the same chat thread: "That's too broad, just do the landing
   page for now."

3. Compare the second `DraftPlan` to the first.

**Expected Results (document actual behavior — this is a known limitation,
not a pass/fail check):** the second plan is an independent draft with no
explicit link back to the first; there is no diff/narrowing UI. If this
changes in a future release, promote this from "known gap" to a real
pass/fail checkpoint.

---
