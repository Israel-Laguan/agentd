# agentd

`agentd` is a local-first daemon for turning approved project plans into durable Kanban tasks.

Foundational baseline contract: [`docs/architecture.md#foundational-baseline-contract`](docs/architecture.md#foundational-baseline-contract).

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

### First Run with Gemini Only

If you only have a Gemini API key:

1. Add `GEMINI_API_KEY=<key>` to `.env` (copy `.env.example`).
2. Set `gateway.order: [gemini]` in `~/.agentd/config.yaml` (or export `AGENTD_GATEWAY_ORDER=gemini`).
3. Run `agentd init` to seed the default agent profiles (empty `provider` / `model` → `gateway.order` cascade):

   ```sh
   agentd init
   ```

4. Start with `--skip-llm-warmup` to avoid a billable startup probe on the free tier:

   ```sh
   agentd start --skip-llm-warmup
   ```

   With only `gemini` in `gateway.order`, seeded profiles already route to Gemini without further setup.
5. (Optional) Pin explicit provider/model on each profile via the agent API:

   ```sh
   # list profile IDs
   curl http://127.0.0.1:8765/api/v1/agents
   # patch each one
   curl -X PATCH http://127.0.0.1:8765/api/v1/agents/<id> \
     -H 'Content-Type: application/json' \
     -d '{"provider":"gemini","model":"gemini-2.5-flash"}'
   ```

6. For dev/smoke testing, set `healing.enabled: false` and `healing.outage_handoff_enabled: false` to suppress self-healing handoffs and `_system` outage tasks. Status defaults omit healing noise: `curl -s 'http://127.0.0.1:8765/api/v1/system/status'`.
7. Materialize creates an empty project workspace. Seed it before workers run: pass `source_path` on `POST /api/v1/projects/materialize`, or rsync into `~/.agentd/projects/<id>/` then call `POST /api/v1/projects/<id>/workspace/ready`. See [`docs/workspace-seeding.md`](docs/workspace-seeding.md).

See [`docs/config-reference.md`](docs/config-reference.md) for all Gemini config keys.

## Development

Run tests through Make (sets `GOCACHE` and `GOMODCACHE` correctly):

```sh
make test PKG=./internal/api/...    # while editing
make check                          # loc + lint + test — before push
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) and [`REVIEW.md`](REVIEW.md) for targets, scoped `PKG`/`RUN`, and troubleshooting.

Use `AGENTD_HOME` or `--home` to override the default `~/.agentd` home directory.

### Docker

There is a `Dockerfile` for the runtime image and a `Dockerfile.test` for running tests inside a container as a non-root `tester` user (it installs `sqlite-libs` and `bash`, the latter because the sandbox executor and plugin hooks shell out to `/bin/bash`). The runtime image uses a multi-stage build:

1. **Build stage** (`golang:1.26-alpine`): compiles a static binary with `CGO_ENABLED=0`.
2. **Runtime stage** (`alpine:latest`): installs `sqlite-libs`, creates a non-root `agentd` user, and copies the binary.

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

Init prints hints for cascade routing, optional `PATCH /api/v1/agents/<id>` to pin provider/model, and (when Gemini is configured) `agentd start --skip-llm-warmup`. See [First Run with Gemini Only](#first-run-with-gemini-only) and [`docs/init-startup.md`](docs/init-startup.md) for the full init/start flow and error surfaces.

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
