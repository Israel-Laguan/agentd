# docker-plan-execute — Error Findings

**Target runtime:** rootless **Podman** + `podman-compose` 1.3.0 (the shell
aliases `docker`/`docker compose` to them). Docker Engine is not a target;
the goal is to get the scenario working on Podman.

**Status (2026-09-27): PASSING.** A clean-state run on rootless Podman
prints `RESULT: PASS (11 passed)`. All issues below are fixed and confirmed
live: infra/compose (E1–E3, P1, P2) and the two app-level failures (F1, F2).

**Topology under test:**
```
tester ──> agentd ──> litellm proxy (model: agentd) ──> mockllm (OpenAI-compatible fake)
```

**Run it (clean state; `down -v` does NOT remove the volume under podman-compose):**
```
cd deploy/docker-plan-execute
docker compose down; podman volume rm -f docker-plan-execute_agentd-projects
docker compose up --build --abort-on-container-exit
```

---

## FIXED — app-level failures (confirmed live on Podman)

### F1 — AGENT_PLAN task was decomposed over and over (agentd bug + stale tester)
- **Observed:** 24 generated `:: Step` subtasks instead of 2 (mockllm logged
  `decomposing plan` ~12 times), parent not BLOCKED, and "no task title
  contains AGENT_PLAN".
- **Root cause 1 (agentd bug, the loop):** documented agentd semantics
  (docs/reference.md "Worker self-breakdown") are: on `too_complex` the parent
  goes BLOCKED with subtasks, and the kanban returns it to READY once every
  child completes (`UnblockBlockedParentsWhenChildrenResolved`). The worker
  then re-ran the resumed parent with the **identical prompt**: nothing told
  the model its work had already been split and done. A model that
  decomposed it once decomposes it again, forever. The mock does so
  deterministically, a real LLM plausibly, and each round costs tokens.
  The depth cap doesn't catch it because the parent's depth stays 0. The run
  never settles (the tester's 150s window just cuts it off).
- **Root cause 2 (tester, "AGENT_PLAN missing"):** nothing renamed the
  parent. `GET /projects/{id}/tasks` is paginated: default 25, sorted
  `created_at DESC`. As the loop added subtasks, the oldest task (the
  AGENT_PLAN parent) fell off page 1. The "25 tasks" figure was the page size.
- **Root cause 3 (tester, stale expectation):** the tester required the
  parent to end BLOCKED. That contradicts the documented resume-to-READY
  behaviour, which has existed since the initial commit, so the check could
  never pass against current agentd.
- **Fix (agentd):** `handleTaskBreakdown` now records a parent comment
  `[worker-breakdown] subtasks=<ids>` after `BlockTaskWithSubtasks`
  (internal/queue/worker/worker_breakdown_rollup.go). When the parent is
  dispatched again, `tryRollUpBreakdown` checks that every listed subtask is
  COMPLETED. If so, it commits the parent as COMPLETED ("completed via N
  breakdown subtasks") and emits `TASK_BREAKDOWN_ROLLUP` without calling the
  LLM. It is hooked into both `dispatchProcess` (single task) and
  `prepareBatchRunnable` (batch path). Parents without a marker, and parents
  resumed while a subtask is still open (e.g. a manual retry), run normally.
  HITL children and materialize dependency edges are unaffected, since only
  the explicit marker triggers a roll-up.
  Tests: internal/queue/worker/worker_breakdown_rollup_test.go (resumed
  parent completes with 0 gateway calls and no new children; same via
  `ProcessBatch`; incomplete subtasks fall through; newest marker wins). The
  first two fail without the hooks.
- **Fix (tester, run-e2e.sh):** fetch `…/tasks?limit=200`. The assertions
  now expect the AGENT_PLAN parent rolled up to COMPLETED, 4 tasks total, all
  COMPLETED, 0 BLOCKED. "Exactly 2 `:: Step` subtasks" is unchanged.
- **Fix (mock, cosmetic):** `decompose_plan` strips the `AGENT_PLAN:` prefix
  including the colon, so subtask titles read `Produce a status report ::
  Step 1 - scaffold` rather than `: Produce a status report :: …`.

### F2 (was R1) — task correlation never reached mockllm (litellm strips it)
- **Observed:** zero `[mockllm] correlation` lines, `task=unknown` in
  PLAN_RESULTS.log, and both evidence assertions failed.
- **(a) agentd:** not the problem. Worker requests set `TaskID`/`AgentID`/
  `Role` (`worker_legacy.go` `command()`), and with `send_task_metadata:
  true` openai.go sends them as `metadata`.
- **(b) litellm 1.100.0: the break.** Confirmed with a request-echo upstream
  behind the same pinned litellm image. The upstream body contained only
  `model`, `messages` and an unknown test key. **`metadata` and `user` were
  both dropped**, and client `x-` headers weren't forwarded either. litellm
  keeps `metadata` for its own logging/spend tracking, which is exactly
  what agentd's M18 option targets ("propagate … to litellm for spend
  correlation"). Unknown top-level keys *are* forwarded upstream.
- **Fix (litellm config, not agentd):** agentd behaves as designed. Emitting
  a non-standard key from agentd would break strict OpenAI-compatible
  endpoints. New `litellm/agentd_correlation.py`, a `CustomLogger`
  `async_pre_call_hook`, copies `metadata.task_id/agent_id/role` into a
  top-level `agentd_metadata` key, which litellm forwards. It is registered
  in `litellm/config.yaml` (`litellm_settings.callbacks`) and mounted at
  `/app/agentd_correlation.py` in compose. mockllm reads `agentd_metadata`
  first, then `metadata` (direct agentd→mockllm), then `user`. When none is
  present it logs the request keys to make the next diagnosis quicker.
  Because the correlation now passes through litellm itself, the e2e also
  proves agentd's metadata reaches litellm.

## FIXED — compose/scenario bugs (confirmed live on Podman)

### E1 — `agentd` container never starts (ENTRYPOINT vs `command:`)
- Image has `ENTRYPOINT ["agentd"]`, and `command:` only overrides CMD, so
  the container ran `agentd sh -c "…"`.
- **Fix:** `entrypoint: ["sh", "-c"]` on the agentd service.

### E2 — `run-e2e.sh` read tasks from `.data.data`
- The response envelope is `{ "status", "data": [tasks], "meta" }`.
- **Fix:** `.data.data` → `.data` (lines 71, 77).

### E3 — `agentd` `command:` was shlex-split, so `sh -c` only ran `agentd`
- A string-form `command:` (`command: >`) is split into an argv array
  (`docker inspect` showed `Cmd = ["agentd","--config",…,"&&","exec",…]`).
  `sh -c` takes only its first argument as the script, so it ran bare
  `agentd`, printed help and exited 0. `init`/`start` never ran.
- **Fix:** `command:` is a single-element YAML list, so the whole line
  reaches `sh -c` as one string. Confirmed: logs show `agentd initialized`,
  then the daemon starts.

## FIXED — Podman-specific (confirmed live)

### P1 — podman-compose mangles array-form healthchecks
- `test: ["CMD", "python", "-c", "…"]` became a broken `CMD-SHELL` string
  (`python' '-c' ''import …`), so mockllm stayed `unhealthy` forever and
  nothing downstream started.
- **Fix:** `mockllm` and `litellm` healthchecks use
  `["CMD-SHELL", "python -c \"…\""]`.

### P2 — agentd got `permission denied` creating project workspaces
- **Cause:** a fresh named volume ends up owned by the container-root uid
  (host uid 1000 under rootless userns). The tester (root) and agentd
  (uid 1000 → host subuid 100999) both mount it, and the tester's root
  ownership wins. `:U` on agentd's mount did **not** fix it on a fresh
  volume (the volume still had `NeedsChown=true` and was owned by 1000:1000
  after the run). It only appeared to work when the volume already existed.
- **Fix:** agentd runs with `userns_mode: "keep-id:uid=1000,gid=1000"`,
  which maps container uid 1000 to the host user, the same host uid that
  owns the volume. That requires `x-podman: { in_pod: false }` at the top
  level, because podman-compose's default shared pod rejects per-container
  `--userns`. Confirmed on a fresh volume: materialize succeeds and all
  tasks run.

## Resolved risks
- **R2 (litellm image/entrypoint):** image is pinned by digest (1.100.0);
  it boots and its healthcheck passes.
- **R3 (tester needs egress for `apk add curl jq`):** fine on this host;
  apk install works. Still a risk only on air-gapped hosts.
- **R4 (`/v1` route prefix):** confirmed. Requests flow
  agentd → litellm `/v1/chat/completions` → mockllm and complete.

---

## Files changed
- `docker-compose.yml`: E1 entrypoint; E3 single-element `command:` list;
  P1 `CMD-SHELL` healthchecks; P2 `userns_mode: keep-id:uid=1000,gid=1000`
  on agentd, plus top-level `x-podman: in_pod: false`; F2 mount of
  `litellm/agentd_correlation.py`.
- `run-e2e.sh`: E2 `.data.data` → `.data`; F1 `?limit=200` and roll-up
  expectations (parent COMPLETED, 4/4 COMPLETED, 0 BLOCKED).
- `litellm/config.yaml` + new `litellm/agentd_correlation.py`: F2 hook.
- `mockllm/server.py`: F2 reads `agentd_metadata`; F1 cosmetic title strip.
- agentd: `internal/queue/worker/worker_breakdown_rollup.go` (new),
  `worker_addons.go`, `worker.go`, `worker_batch_process.go`, and
  `worker_breakdown_rollup_test.go` (new): F1 roll-up.

## Final tester output (clean state, 2026-09-27)
```
  PASS agentd is healthy
  PASS plan materialized (project=920685b4-b76c-404a-879e-c522af97ef43)
  PASS kanban settled (4 tasks, 4 completed, 0 blocked)
  PASS generated plan subtasks present (2)
  PASS plan container task present (title contains AGENT_PLAN)
  PASS AGENT_PLAN parent rolled up to COMPLETED after its subtasks
  PASS all 2 generated plan subtask(s) completed (GEN_COMPLETED == GEN_SUBTASKS)
  PASS original direct greeting task is COMPLETED
  PASS strict: all 4 tasks COMPLETED, none BLOCKED
[e2e] reading execution evidence: /home/agentd/projects/920685b4-b76c-404a-879e-c522af97ef43/PLAN_RESULTS.log
  PASS execution evidence present for the direct task
  PASS execution evidence present for generated subtask(s) (2)
[e2e] ----- PLAN_RESULTS.log -----
    [agentd] task=c3ab1d7f-5daf-489d-8677-748da173e62f title=Generate a greeting script executed via litellm proxy
    [agentd] task=cbd8d51d-9fe9-47de-a39d-48ad954e13cc title=Produce a status report :: Step 2 - implement executed via litellm proxy
    [agentd] task=a1b1feae-1a8e-4b9e-ac58-69a7394e47aa title=Produce a status report :: Step 1 - scaffold executed via litellm proxy

RESULT: PASS (11 passed)
```
mockllm logged `decomposing plan` exactly once, and a `correlation
task_id=<real id> agent_id=default role=worker` line for all 4 worker calls.
