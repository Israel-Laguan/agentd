# docker-plan-execute — Error Findings

**Status:** Static analysis only (this environment has no `docker`, so the
live run could not be observed). Two blocking bugs were found by cross-referencing
the scenario files against the agentd source, and were fixed in place. The
remaining items are risks/uncertainties that should be confirmed on the next
real run (see "Next chat" section).

**Topology under test:**
```
agentd  ──>  litellm proxy (model: mock/agentd)  ──>  mockllm (OpenAI-compatible fake)
```

---

## CONFIRMED BLOCKING ERRORS (fixed in this chat)

### E1 — `agentd` container never starts (ENTRYPOINT vs `command:`)
- **File:** `docker-compose.yml` (agentd service), `Dockerfile` (`ENTRYPOINT ["agentd"]`)
- **Root cause:** The image defines `ENTRYPOINT ["agentd"]` and `CMD ["init"]`.
  In Compose, `command:` only overrides `CMD`, NOT `entrypoint`. So the
  container actually executes:
  ```
  agentd sh -c "agentd --config /etc/agentd/config.yaml init && exec agentd --config /etc/agentd/config.yaml start --skip-llm-warmup -v"
  ```
  i.e. `agentd` is invoked with subcommand `sh` (which does not exist), so the
  daemon never starts. The `agentd` healthcheck (`wget ... /api/v1/system/status`)
  then never passes, so the `tester` (which `depends_on: agentd: healthy`) never
  launches, and `docker compose up --abort-on-container-exit` fails/exits.
- **Fix applied:** Added `entrypoint: ["sh", "-c"]` to the `agentd` service so the
  `command:` shell one-liner runs as intended.

### E2 — `run-e2e.sh` reads tasks from the wrong JSON path (`.data.data`)
- **File:** `run-e2e.sh` lines 71 and 77
- **Root cause:** The `GET /api/v1/projects/{id}/tasks` response is wrapped as
  `{ "status": "success", "data": [ ...tasks... ], "meta": {...} }`
  (see `internal/api/httpx/response.go` `Envelope` + `TaskHandler.ListByProject`
  → `httpx.WriteSuccess(w, …, h.attachAgents(...), meta)`). The script used
  `.data.data`, which on an array evaluates to `[null, null, …]`, so every
  downstream assertion silently breaks:
  - `COMPLETED` → 0 → "no tasks completed" FAIL (even though tasks ran)
  - `PLAN_CONTAINER` → 0 → "AGENT_PLAN task missing" FAIL
  - `TOTAL` happens to equal array length (a false-positive PASS)
- **Fix applied:** Changed both occurrences from `.data.data` to `.data`.

---

## CONFIRMED MINOR (non-blocking, left as-is)

### E3 — `send_task_metadata` emits a startup warning
- **File:** `agentd/config.yaml` (`gateway.providers.options.send_task_metadata: true`)
- **Detail:** In `internal/gateway/providers/openai.go` the option is passed to
  `warnUnknownOptions("openai", []string{"send_task_metadata"}, …)`, which logs
  `slog.Warn("unknown provider option ignored", …)`. The option is still honored
  (it builds the `metadata` map at lines 55–57), so it is cosmetic only. No fix
  needed, but expect one WARN line in the agentd logs.

---

## UNCERTAIN / RISK ITEMS (verify on next real run)

These could not be confirmed without a live run. They are the most likely
candidates for the next round of failures.

### R1 — litellm may strip the `metadata` field (M18 correlation claim)
- **File:** `mockllm/server.py` (reads `body.get("metadata")`), `agentd` OpenAI
  provider (sends `metadata: {task_id, agent_id, role}`).
- **Risk:** agentd sends `metadata` as a top-level OpenAI request field. litellm
  treats `metadata` as its own spend-tracking field and may NOT forward it to the
  upstream `mockllm`. If so, the correlation log line in mockllm will never print.
  The script does **not** assert on metadata, so this does not fail the e2e — but
  the "What it proves" bullet about correlation flowing through litellm would be
  unfounded. **Verify** by grepping `docker compose logs mockllm` for
  `[mockllm] correlation task_id=`. If absent, the metadata is being dropped by
  litellm.

### R2 — litellm image entrypoint / tag
- **File:** `docker-compose.yml` (`image: litellm/litellm:main`, `command:
  ["--config", "/app/config.yaml", "--host", "0.0.0.0", "--port", "4000"]`).
- **Risk (already flagged by author):** If the `litellm/litellm:main` image's
  entrypoint is not `litellm`, this `command` is wrong. If it fails, the `litellm`
  healthcheck (`/health/liveliness`) never passes and `agentd` never starts.
  **Verify** by checking `docker compose logs litellm` for a clean "litellm proxy
  running on http://0.0.0.0:4000" line. If the image changed, adjust `command:`
  (e.g. `command: ["litellm", "--config", …]` or set an explicit `entrypoint`).

### R3 — `tester` needs network egress for `apk add curl jq`
- **File:** `run-e2e.sh` (installs `curl` + `jq` via `apk` if missing).
- **Risk:** `alpine:3.20` does not ship `curl`/`jq`. The script installs them at
  runtime, which requires package-registry network access from the `tester`
  container. In an offline/airgapped host the `tester` aborts before any check.
  **Mitigation if offline:** base the tester on `curlimages/curl` + a `jq` image,
  or pre-bake an image with both installed.

### R4 — litellm route prefix (`/v1/...`)
- **File:** `litellm/config.yaml` (`api_base: http://mockllm:8000/v1`) and
  `agentd/config.yaml` (`base_url: http://litellm:4000/v1`).
- **Risk:** Low. litellm serves both `/chat/completions` and `/v1/chat/completions`;
  the mock's `do_POST` checks `path.rstrip("/").endswith("/chat/completions")` so
  `/v1/chat/completions` matches. Same for `/v1/models`. Confirmed-correct by
  inspection, but worth a sanity check on the first live run.

---

## Files changed this chat
- `deploy/docker-plan-execute/docker-compose.yml` — added `entrypoint: ["sh","-c"]`
  to the `agentd` service (fixes E1).
- `deploy/docker-plan-execute/run-e2e.sh` — `.data.data` → `.data` (fixes E2).

## Next chat (retry plan)
1. Run `cd deploy/docker-plan-execute && docker compose up --build --abort-on-container-exit`.
2. If it passes: done.
3. If it fails, capture `docker compose logs` for `mockllm`, `litellm`, `agentd`,
   and the `tester` output. Most likely remaining failures are R1/R2/R3 above.
4. Open a new chat with those logs; apply targeted fixes; re-run.
