# agentd Test Troubleshooting

> Part of the [agentd testing plan](../../TESTING_PLAN.md).

## Troubleshooting

### Tasks stuck in PENDING (never dispatched)

Materialized plans **without** a `source_path` or explicit
`start_empty_workspace: true` intentionally create tasks in `PENDING` and keep
them unclaimable until an operator signals workspace readiness (see
`ProjectService.MaterializePlan` in `internal/services/project_service.go`). To unlock:

```bash
PROJECT_ID="replace-with-project-id"

# 1. Seed at least one file into the project workspace (IsWorkspacePopulated requires >=1 entry)
podman exec agentd_agentd_1 sh -c 'echo "# workspace" > "/home/agentd/projects/$1/README.md"' sh "$PROJECT_ID"

# 2. Transition PENDING -> READY (dispatch happens within ~10s after this)
curl -s -X POST "http://localhost:8765/api/v1/projects/${PROJECT_ID}/workspace/ready"
```

Chat-created plans now send `start_empty_workspace: true`, so they use the
explicit empty-workspace flow. Plans using the two-phase seeded-workspace flow
must omit the flag, seed content, and then call `workspace/ready`.

Note: the boot log field `scheduler_enabled: false` refers to the optional **agentic cron
scheduler** (`agentic.scheduler.enabled`, defaults to `false`) — it is unrelated to normal task
dispatch. The queue daemon's `taskLoop` runs regardless.

### Tasks fail with FAILED_REQUIRES_HUMAN / poison_pill_handoff

After auto-retries, an executable task can end in `FAILED_REQUIRES_HUMAN`. Inspect the daemon
logs and the project workspace before retrying. A sandbox path violation means the persisted
`workspace_path` is stale or outside the configured `projects_dir`:

``` text
sandbox path violation: <workspace_path> escapes /home/agentd/projects
```

Workspace paths are absolute and host-specific. If `AGENTD_HOME` or `projects_dir` changed, an
operator must move the existing project directories under the configured root before restarting;
the daemon intentionally rejects stale or outside-root rows rather than relocating them. After
repairing the path, retry via `POST /api/v1/tasks/{id}/retry` and require `COMPLETED` plus a
matching task-ID line in the project's `PLAN_RESULTS.log` as execution evidence.

### If LiteLLM is not running

- Tasks will fail with LLM errors

- Check logs for "connection refused" or "LLM error"

- Discover the real model alias from the operator’s authenticated `/v1/models` response, then set `LITELLM_BASE_URL`, `LITELLM_API_KEY`, and `LITELLM_MODEL` for agentd.

- Check litellm config mount: `./devenv/litellm/config.yaml:/app/config.yaml:ro` (plus `./devenv/litellm/agentd_correlation.py`, which that config loads as a callback)

### If web shows "Not Connected"

- Check if daemon is running on port 8765: `curl -s http://localhost:8765/api/v1/system/status`

- Check browser console for errors

- Verify `NEXT_PUBLIC_API_URL` is `http://localhost:8765` (not `host.docker.internal` - that
  hostname is not defined on Linux and is only needed if fetches happened from inside the
  container, but the web app runs client-side in the browser)

- If web returns 500 with `@tailwindcss/postcss` module error: the container's `NODE_ENV` is
  `production` and `npm install` skipped devDeps. Set `NODE_ENV: "development"` in the compose
  file and restart the web container.

### If tasks don't appear

- Check daemon logs for errors: `podman logs agentd_agentd_1`

- Verify database has tasks: use `GET /api/v1/projects/{id}/tasks`; the agentd image
  does not ship the `sqlite3` CLI. If direct SQL is required, run a separate utility
  container against the `agentd-data` volume.

- For smoke tests, verify LiteLLM is reachable: `curl -s -H "Authorization: Bearer $LITELLM_API_KEY" http://localhost:4000/v1/models`

### Healthchecks show unhealthy

- podman-compose 1.3.0 has a known healthcheck quoting bug with exec-form (`CMD`) tests.
  All healthchecks in this compose file use `CMD-SHELL` (string form) to work around it.

- If a service stays unhealthy, run:

  ```bash
  podman logs <service>_<n>
  podman inspect <service>_<n> --format '{{json .State.Health}}'
  ```

- Healthchecks use `depends_on: condition: service_started` (not `service_healthy`) so the

  stack can always start even if a healthcheck is flaky.

### Web container permission issues (EACCES)

- The web container runs as `root` (uid 0) inside the container. With rootless podman, uid 0
  maps to the host user (uid 1000), so host file ownership is preserved and this is safe.

- `npm install` and `next dev` both need write access to the bind-mounted `./web` directory
  (for `node_modules` and `.next` respectively), hence `user: root` is required.

### If chat doesn't trigger plan flow

- Use action words in the prompt: "build", "create", "implement", "design", "plan", "scrape", etc.

- See the LiteLLM proxy logs for model routing and the agentd logs for plan generation
