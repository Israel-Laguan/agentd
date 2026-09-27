# SP-007: Folder architecture for local-dev, container, test and tooling files

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Time box | 1 day |
| Links | docs/litellm-integration.md (Container deploy notes), docs/container-development.md, Makefile, .github/workflows/ci-go.yml |

## Question

Where should files for local testing with Podman/Docker, CI helpers, test
harnesses and repo tooling live, and what should the folders be called, so a
newcomer can tell from the name what each one is for?

Today it's split across the root, `dev/`, `scripts/` and `e2e/`, with no rule
(audit from 2026-09-27):

| Location | What it actually is |
| --- | --- |
| `docker-compose.dev.yml` (root) + `dev/` | Podman stack for local web-UI testing and its support files (mock LLM, litellm config + hook, agentd config) |
| `Dockerfile` (root) | Production image |
| `Dockerfile.test` (root) | Image that runs the Go suite (`make podman-test`) |
| `Dockerfile.init-test` + `scripts/verify-init.sh` | `agentd init` check in a container; nothing references them |
| `e2e/http_test.go` | Not e2e: in-process `httptest` API tests, like `internal/api/tests/feature/`; also runs under `make test` |
| `scripts/*.sh` | Mix of CI helpers (`verify_gomod.sh`, `ci_report_failure.sh`), manual QA (`chat-kanban-qa.sh`, `llm-smoke.sh`), dev launcher (`test-env.sh`) |
| `scripts/checkloc`, `scripts/checkminfunc` | Go programs (linters run by CI), not scripts |
| `scripts/demo/` | Demo scripts with their own mock provider |
| `scripts/run-tests.sh` → `integration-test.sh` | Container API test that writes reports into `docs/`; nothing references them |

Other issues found during the audit:

- Three mock LLMs: `scripts/mock_llm.py`, `scripts/demo/mock-provider.py`, `dev/mockllm/server.py`.
- Tracked files that `.gitignore` excludes: `scripts/__pycache__/*.pyc`, `scripts/demo/__pycache__/*.pyc`, `scripts/checkminfunc/baseline.bak`.
- `make folder-audit` runs `./scripts/folder_audit`, which does not exist.

## Output

A short decision doc (or an ADR) with the target layout, the naming rule for
each folder, and a migration checklist, ready to run as one follow-up task.

- [ ] Compare conventions: golang-standards/project-layout (`scripts/`, `build/`, `deployments/`, `test/`, `tools/`), Kubernetes/CNCF (`hack/`, `test/e2e`, `test/integration`), Grafana (`devenv/`).
- [ ] Decide the local-dev container home and its name (`dev/` vs `devenv/` vs `deployments/`), and whether `docker-compose.dev.yml` and `Dockerfile.test` move into it.
- [ ] Decide where Go tooling lives (`tools/` vs `internal/tools/` vs staying under `scripts/`).
- [ ] Decide the fate of `e2e/`: fold into `internal/api/tests/` or rename, and what `make test-e2e` means afterwards.
- [ ] Pick one mock LLM for dev, demos and the launcher.
- [ ] Confirm with the owner whether the unreferenced files (`Dockerfile.init-test`, `verify-init.sh`, `run-tests.sh`, `integration-test.sh`) are dead.
- [ ] Migration checklist covering every path to update: CI (`ci-go.yml` watches `scripts/**`, runs `scripts/verify_gomod.sh`, reads the `checkminfunc` baseline), Makefile targets, `.dockerignore`, docs, TESTING_PLAN.md, and the `podman-runtime-target` note.

## Out of scope

- Doing the moves. That's a follow-up task once the layout is agreed.
- `internal/` package structure and `web/`.

## Notes

- Whatever the layout, it must keep working on rootless Podman + podman-compose 1.3.0 (see "Container deploy notes" in docs/litellm-integration.md).
- The production `Dockerfile` conventionally stays at the root.
