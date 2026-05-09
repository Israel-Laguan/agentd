# agentd

`agentd` is a local-first daemon for turning approved project plans into durable Kanban tasks.

Phase 1 hardening baseline: [`docs/phase1-skeleton.md`](docs/phase1-skeleton.md).

## Core Components

- Domain models and shared interfaces in `internal/models`.
- SQLite-backed Kanban store with task DAG, comments, and events in `internal/kanban`.
- AI gateway with provider fallback (OpenAI, Anthropic, Ollama, llama.cpp, AI Horde) in `internal/gateway`.
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

Use `AGENTD_HOME` or `--home` to override the default `~/.agentd` home directory.

### Docker

There is a single `Dockerfile` — no separate dev Dockerfile exists. It uses a multi-stage build:

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

`init` creates directories (`projects/`, `uploads/`, `archives/`), initializes the SQLite database, writes `agentd.crontab`, and seeds the `default`, `researcher`, and `qa` agent profiles.

## Configuration

Precedence (highest wins):

1. CLI flags (`--home`, `--workers`)
2. Explicit `--config <file>` (for keys present in that file)
3. `AGENTD_*` environment variables
4. Auto-discovered `<home>/config.yaml`
5. Compiled defaults

See [`config.reference.yaml`](config.reference.yaml) for every available key. See [`docs/reference.md`](docs/reference.md) for config key defaults, task states, event types, and feature catalog.

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
| [`docs/guardrails.md`](docs/guardrails.md) | Size limits, layer boundaries, quality workflow (human-facing) |
| [`docs/phase1-skeleton.md`](docs/phase1-skeleton.md) | Phase 1 hardening baseline contract |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to contribute |
| [`STYLEGUIDE.md`](STYLEGUIDE.md) | Coding conventions and style guide |

## Maintenance Scripts

Generate folder-size audit report (default output: `docs/folder-size-audit.md`):

```sh
python3 scripts/folder_audit.py
```
