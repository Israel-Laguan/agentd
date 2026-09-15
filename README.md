# agentd

Local daemon that turns an approved plan into a durable Kanban board and runs sandboxed workers against it — model-agnostic, SQLite source of truth.

- **What it is:** a local-first **control-plane harness** (board + workers + memory). Human-approved plans become durable tasks; workers run against that board with heartbeats, retries, and handoffs.
- **What it is not:** another coding CLI or chat loop that happens to edit files. It does not compete with Claude Code / OpenHands / Cursor on editor UX or SWE-bench.
- **Why local-first:** SQLite is the source of truth, one Go binary, cheap/local providers welcome (Ollama, llama.cpp, LiteLLM, Poolside, …). Unattended runs degrade into board tickets (`HUMAN`), not a stuck transcript.

Foundational baseline contract: [`docs/architecture.md#foundational-baseline-contract`](docs/architecture.md#foundational-baseline-contract).

Why agentd (vs Claude Code / OpenHands / agent-kanban / HAR): [`docs/why-agentd.md`](docs/why-agentd.md).

Product plan / positioning: [`docs/product-plan.md`](docs/product-plan.md). Sprint board: [`tasks/README.md`](tasks/README.md).

## Core Components

- Domain models and shared interfaces in `internal/models`.
- SQLite-backed Kanban store with task DAG, comments, and events in `internal/kanban`.
- AI gateway with provider fallback (OpenAI, Anthropic, Gemini, Ollama, llama.cpp, AI Horde) in `internal/gateway`.
- Sandboxed command execution with permission detection in `internal/sandbox`.
- Worker-pool queue with heartbeat, retry, and phase planning in `internal/queue`.
- Two-phase log archival and memory curation in `internal/memory`.
- HTTP API with SSE streaming in `internal/api`.
- API service layer (project materialization, task operations, system status) in `internal/services`.
- Cron-driven background jobs (task dispatch, intake, heartbeat, disk watchdog, memory curator).
- Dynamic Workforce Manager: agent registry, live SSE event pulse, and manager loop for task reassignment, split, and retry. See [Journey 5](docs/reference.md).

CLI commands: `init`, `start`, `status`, `comment`, `config`, `project`, `suggest`, `ask`.

## Quickstart

```sh
make build
./bin/agentd init
```

**Governance demo (approve → board → HUMAN):** see [`docs/demo.md`](docs/demo.md) — about 10 minutes with a connector you control.

**Harness reliability (restart mid-task):** see [`docs/harness-reliability.md`](docs/harness-reliability.md) and `scripts/demo/restart-mid-task.sh`.

### First Run

**Recommended: LiteLLM / Poolside (or local mock)**

The simplest path is a connector you control — not a stale model id baked into docs.

**Option A — LiteLLM (recommended):**

1. Start a local LiteLLM proxy (see [docs/llm-connector-strategy.md](docs/llm-connector-strategy.md)).
2. Add `LITELLM_API_KEY=<key>` to `.env` (copy `.env.example`).
3. Point `gateway.order` at LiteLLM in `~/.agentd/config.yaml`:

   ```yaml
   gateway:
     order: [litellm]
     providers:
       - name: litellm
         adapter: openai
         base_url: "http://127.0.0.1:4000/v1"
         api_key_env: LITELLM_API_KEY
         model: "poolside/laguna-m.1"   # or another alias from your LiteLLM config
   ```

4. Run `agentd init` to seed default agent profiles (empty `provider` / `model` → `gateway.order` cascade):

   ```sh
   agentd init
   ```

5. Start the daemon:

   ```sh
   agentd start --skip-llm-warmup
   ```

**Option B — local mock (fully offline):** any process that speaks `POST /v1/chat/completions`. See [`docs/demo.md`](docs/demo.md) for a worked example. Minimal config to point at a local mock:

   ```yaml
   gateway:
     order: [mock]
     providers:
       - name: mock
         adapter: openai
         base_url: "http://127.0.0.1:18080/v1"
         model: "mock-model"
   ```

**Option C — Gemini only:** if you only have a Gemini API key, add `GEMINI_API_KEY=<key>` to `.env` and set `gateway.order: [gemini]` (or export `AGENTD_GATEWAY_ORDER=gemini`). Run `agentd init` then `agentd start --skip-llm-warmup` to start the daemon without a billable startup probe on the free tier. See [`docs/config-reference.md`](docs/config-reference.md) for all Gemini config keys.

**(Optional)** Pin explicit provider/model on each profile via the agent API:

```sh
# list profile IDs
curl http://127.0.0.1:8765/api/v1/agents
# patch each one
curl -X PATCH http://127.0.0.1:8765/api/v1/agents/<id> \
  -H 'Content-Type: application/json' \
  -d '{"provider":"<provider>","model":"<model>"}'
```

**Dev/smoke testing:** set `healing.enabled: false` and `healing.outage_handoff_enabled: false` to suppress self-healing handoffs and `_system` outage tasks. Status defaults omit healing noise: `curl -s 'http://127.0.0.1:8765/api/v1/system/status'`.

**Workspace seeding:** materialize creates an empty project workspace. Seed it before workers run: pass `source_path` on `POST /api/v1/projects/materialize`, or rsync into `~/.agentd/projects/<id>/` then call `POST /api/v1/projects/<id>/workspace/ready`. See [`docs/workspace-seeding.md`](docs/workspace-seeding.md).

## Development

Run tests through Make (sets `GOCACHE` and `GOMODCACHE` correctly):

```sh
make test PKG=./internal/api/...    # while editing
make check                          # loc + minfunc + lint + test + lint-docs — before push
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) and [`REVIEW.md`](REVIEW.md) for targets, scoped `PKG`/`RUN`, and troubleshooting.

Use `AGENTD_HOME` or `--home` to override the default `~/.agentd` home directory.

### Docker

There is a `Dockerfile` for the runtime image and a `Dockerfile.test` for running tests inside a container as a non-root `tester` user (it installs `sqlite-libs` and `bash`, the latter because the sandbox executor and plugin hooks shell out to `/bin/bash`). The runtime image uses a multi-stage build:

1. **Build stage** (`golang:1.26-alpine`): compiles a static binary with `CGO_ENABLED=0`.
2. **Runtime stage** (`alpine:3.21@sha256:f27cad9117495d32d067133afff942cb2dc745dfe9163e949f6bfe8a6a245339`): installs `sqlite-libs`, creates a non-root `agentd` user, and copies the binary.

```sh
docker build -t agentd .
docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd init
docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd start -v
```

### Init Command

```sh
agentd init                    # Initialize with defaults (~/.agentd)
agentd init --home /custom/dir # Use custom home directory
agentd -v init                 # Initialize with verbose logging
```

`init` creates directories (`projects/`, `uploads/`, `archives/`), initializes the SQLite database, writes `agentd.crontab`, and seeds the `default`, `researcher`, and `qa` agent profiles with empty `provider` / `model` (tasks follow `gateway.order`). Re-running `init` **preserves** any operator PATCH to those profiles; use `--reset-profiles` to force defaults.

Init prints hints for cascade routing, optional `PATCH /api/v1/agents/<id>` to pin provider/model, and (when Gemini is configured) `agentd start --skip-llm-warmup`. See [First Run](#first-run) and [`docs/init-startup.md`](docs/init-startup.md) for the full init/start flow and error surfaces.

## Configuration

Precedence (highest wins):

1. CLI flags (`--home`, `--workers`)
2. Explicit `--config <file>` (for keys present in that file)
3. `AGENTD_*` environment variables
4. Auto-discovered `<home>/config.yaml`
5. Compiled defaults

A `.env` file in the current working directory (and `<home>/.env`) is merged into `AGENTD_*` values at startup without modifying your shell environment. Process environment variables still win over `.env`. When an env value disagrees with the same key in `config.yaml` (for example `AGENTD_GATEWAY_ORDER` in the repo `.env` vs `gateway.order` in `~/.agentd/config.yaml`), agentd logs `config: key overridden by env` at INFO on startup. Inspect resolved values and sources with `agentd config show`.

See [`config.reference.yaml`](config.reference.yaml) for every available key (copy-paste template). See [`docs/config-reference.md`](docs/config-reference.md) for extended configuration documentation and [`docs/reference.md`](docs/reference.md) for config key defaults, task states, event types, and feature catalog.

## Chat Intake

`POST /v1/chat/completions` accepts OpenAI-style chat messages, routes the last user message through Frontdesk, and returns structured JSON (plans, status reports, or clarification payloads). See [`docs/frontdesk.md`](docs/frontdesk.md) for the full decision flow.

## Cron Schedule

`agentd init` creates `<home>/agentd.crontab` with default background job schedules. See [`docs/reference.md`](docs/reference.md) for the full list.

## Security

Run spawned agents under a non-sudoer system user. `agentd` blocks commands that invoke `sudo`, and permission failures are handed off as HUMAN tasks.

## Documentation

| Document | Description |
| --- | --- |
| [`GUARDRAILS.md`](GUARDRAILS.md) | Agent safety protocol — Signs architecture for autonomous agent constraints |
| [`docs/architecture.md`](docs/architecture.md) | System nodes, data flows, failure analysis, architectural invariants |
| [`docs/architecture-flows.md`](docs/architecture-flows.md) | Extended flows: Manager's Loop, Memory Recall |
| [`docs/frontdesk.md`](docs/frontdesk.md) | Chat intake decision flow, package boundaries, interface seams |
| [`docs/reference.md`](docs/reference.md) | Feature catalog, task states, event types, config key reference |
| [`docs/workspace-seeding.md`](docs/workspace-seeding.md) | Workspace seeding: `source_path` on materialize and `workspace/ready` two-phase flow |
| [`docs/openai-compatible-providers.md`](docs/openai-compatible-providers.md) | Using Groq, Together AI, Poolside, and other OpenAI-compatible cloud vendors |
| [`docs/llamacpp-quickstart.md`](docs/llamacpp-quickstart.md) | Local inference quickstart (llama.cpp, LM Studio, vLLM, Ollama) |
| [`docs/guardrails.md`](docs/guardrails.md) | Size limits, layer boundaries, quality workflow (human-facing) |
| [`docs/architecture.md#foundational-baseline-contract`](docs/architecture.md#foundational-baseline-contract) | Foundational baseline contract |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to contribute |
| [`REVIEW.md`](REVIEW.md) | PR review checklist (build, lint, tests, Go cache) |
| [`STYLEGUIDE.md`](STYLEGUIDE.md) | Coding conventions and style guide |

## Maintenance Scripts

Generate an ad-hoc folder-size audit report:

```sh
go run ./scripts/folder_audit --out /tmp/folder-size-audit.md
```

The report ranks folders by non-test Go files and separately calls out test-heavy folders. It is intentionally not checked in; use it for temporary analysis.
