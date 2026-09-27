# agentd Testing Plan

This document outlines the testing plan to verify agentd functionality through the web interface.

## Prerequisites

- Podman (or Docker) and Podman Compose installed
- Ports 3000, 4000, 8765 available

## Quick Start with Podman Compose

The easiest way to run the full stack locally:

```bash
# Build and start all services
podman compose -f docker-compose.dev.yml up --build -d

# Open http://localhost:3000

# To stop
podman compose -f docker-compose.dev.yml down

# To rebuild after code changes:
podman compose -f docker-compose.dev.yml build --no-cache
```

> **Note:** If the web container fails to start with `Cannot find module '@tailwindcss/postcss'`,
> ensure the `web` service has `NODE_ENV: "development"` set in `docker-compose.dev.yml`.
> The node:22-alpine image ships `NODE_ENV=production`, which causes `npm install` to skip
> devDependencies. With `NODE_ENV=development`, the full dependency tree (including devDeps
> like `@tailwindcss/postcss`) is installed before `next dev` starts.
>
> **Note:** The `agentd` service in this compose file starts with `--skip-llm-warmup`.
> Because of that, a green `/health` or `/api/v1/system/status` does **not** prove LiteLLM
> connectivity. See Checkpoint 0 for the isolated warmup run that does prove connectivity.

Services:
- **litellm** (port 4000) - LLM proxy/router
- **agentd** (port 8765) - Daemon
- **web** (port 3000) - Next.js frontend

## Running the Test Environment (Manual)

The compose stack is the recommended way to run the full local test environment
(`podman compose -f docker-compose.dev.yml up --build -d`). It wires agentd
through LiteLLM as the LLM provider.

### Terminal 1: Start LiteLLM (compose handles this)
```bash
podman compose -f docker-compose.dev.yml up -d litellm
```

### Terminal 2: Start agentd daemon
Build first:
```bash
make build
```
Then run with a config whose gateway provider points at LiteLLM and
`warmup_enabled: true` if you want to prove LLM connectivity (Checkpoint 0).
With `--skip-llm-warmup` the daemon boots without proving provider connectivity.

### Terminal 3: Start web frontend
```bash
cd web
NEXT_PUBLIC_USE_MOCK=false npm run dev
```

### Terminal 4: Access the web interface
Open browser to: **http://localhost:3000**

---


## Test documentation map

This plan is split so each document stays focused and reviewable:

| Document | Covers |
| --- | --- |
| [Test Checkpoints](docs/testing/checkpoints.md) | CP0–CP4, the per-checkpoint steps and acceptance evidence |
| [Plan Materialization Edge Cases and Plan Refinement](docs/testing/materialization-and-plan-refinement.md) | CP3b materialization edge cases and the plan-refinement known gap |
| [QA Scripts and Manual Browser Verification](docs/testing/qa-and-browser-verification.md) | `chat-kanban-qa.sh`, privileged handoff QA, manual click-through |
| [Test Execution Results](docs/testing/results.md) | Historical run outcomes and known gaps confirmed live |
| [Test Troubleshooting](docs/testing/troubleshooting.md) | PENDING dispatch, warmup, workspace-seeding, and routing failures |

Run the prerequisites and quick start above, then work through the
checkpoints. Append new outcomes to the results document — do not grow this
index.
