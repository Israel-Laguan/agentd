# 10-minute governance demo

Show **approve → board → HUMAN handoff**, not “it wrote an API.”

Throwaway home recommended: `export AGENTD_HOME=/tmp/agentd-demo` (or `agentd --home …` on every command).

## 0. Build

```sh
make build
./bin/agentd --home "$AGENTD_HOME" init
```

## 1. Controlled connector (LiteLLM / Poolside — preferred)

Do **not** hardcode stale Gemini model ids. Point agentd at a connector you control.

**Option A — LiteLLM (recommended if you already run it):**

```yaml
# $AGENTD_HOME/config.yaml
api:
  address: "127.0.0.1:8765"
gateway:
  order: [litellm]
  providers:
    - name: litellm
      adapter: openai
      base_url: "http://127.0.0.1:4000/v1"
      api_key_env: LITELLM_API_KEY   # or LITELLM_MASTER_KEY for local demos
      model: "poolside/laguna-m.1"   # or another alias from your LiteLLM config
healing:
  enabled: true
  outage_handoff_enabled: true
breaker:
  handoff_after: 10s   # short for demos; default is 2m
```

Put the matching key in `$AGENTD_HOME/.env`. Start LiteLLM separately.

**Option B — local mock OpenAI** (fully offline; good for CI/docs rehearsals): any process that speaks `POST /v1/chat/completions` and returns intent / scope / DraftPlan / worker JSON. See SP-003 run log for a minimal mock shape.

```sh
# agentd start blocks in the foreground (API server + worker loop).
# Run it in a SECOND terminal, or background it — do not run it inline
# before the ask/approve/board steps.
./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup

# background option (logs to file):
# ./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup &> "$AGENTD_HOME/agentd.log" &
```

Wait for `API server listening on 127.0.0.1:…` in the daemon terminal, then proceed.

API default in this doc: `http://127.0.0.1:8765` (or whatever you set in `api.address`).

## 2. Ask → approve → materialize

```sh
./bin/agentd --home "$AGENTD_HOME" ask "Create a tiny project that writes hello.txt with hello world."
# approve with Y
```

Expected: proposed tasks printed, then `project started`.

## 3. Seed workspace + unlock tasks

Materialize leaves root tasks `PENDING` until the workspace is non-empty:

```sh
PROJECT_ID=…   # from ask output or: curl -s http://127.0.0.1:8765/api/v1/projects
mkdir -p "$AGENTD_HOME/projects/$PROJECT_ID"
echo seed > "$AGENTD_HOME/projects/$PROJECT_ID/README.md"
curl -sS -X POST "http://127.0.0.1:8765/api/v1/projects/$PROJECT_ID/workspace/ready"
```

Empty workspace → `409 Conflict`. After ready, task state should be `READY`.

## 4. Watch the board

```sh
curl -sS "http://127.0.0.1:8765/api/v1/projects/$PROJECT_ID/tasks"
# optional: ./bin/agentd --home "$AGENTD_HOME" status
```

Workers claim `READY` work on the cron dispatch interval.

## 5. HUMAN beat — inject connector failure

Keep `healing.enabled: true`. Break the connector on purpose:

1. Stop LiteLLM / mock, **or** rewrite config so the only `gateway.order` entry uses  
   `base_url: "http://127.0.0.1:1/v1"` (nothing listening).
2. Restart `agentd start` if config is read only at boot.
3. Ensure a `READY` task exists (seed another tiny project if needed).

Within a few dispatch cycles (breaker trips after **3** unreachable failures by default):

| Signal | Expected |
| --- | --- |
| Breaker | `OPEN` (`GET /api/v1/system/status`) |
| Parent task | `BLOCKED` |
| HUMAN child | title starts with `Manual review required: AI providers unavailable` |
| Cause | `ErrLLMUnreachable` / connection refused |

```sh
curl -sS "http://127.0.0.1:8765/api/v1/projects/$PROJECT_ID/tasks?include_healing=true"
curl -sS "http://127.0.0.1:8765/api/v1/system/status"
```

That is the demo climax: **governance**, not a coding flex.

Optional encore (not required): permission/`sudo` sandbox → HUMAN. Prefer connector inject for a reliable script.

## 6. Success checklist

- [ ] Plan required explicit approval before tasks existed
- [ ] Board (SQLite), not the chat transcript, held task state
- [ ] Forced provider outage produced a **HUMAN** board item and blocked parent
- [ ] No claim that agentd “won” as a coding agent

## Related

- Spike evidence / notes: [SP-003](../tasks/sprints/S01-positioning-and-demo/spikes/SP-003-demo-path-dry-run.md)
- Positioning: [why-agentd.md](why-agentd.md), [product-plan.md](product-plan.md)
- Workspace seeding: [workspace-seeding.md](workspace-seeding.md)
- Connector strategy: [llm-connector-strategy.md](llm-connector-strategy.md)
