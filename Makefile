GO ?= go
GOLANGCI_LINT ?= $(shell $(GO) env GOPATH)/bin/golangci-lint
# Comma-separated patterns for merged coverage (default: entire module). Override to narrow the denominator, e.g. internal-only: $(shell go list ./internal/... | paste -sd, -)
COVERPKG ?= ./...

.PHONY: build test test-e2e test-bins coverage run tidy lint lint-install loc minfunc minfunc-accept check podman-test lint-md lint-links lint-docs smoke-contract dev-build dev-up dev-down dev-clean dev-logs

# Workspace-local GOCACHE; default GOMODCACHE to the user module cache (agent
# sandboxes often set an empty GOMODCACHE and break go test / make build).
GOMODCACHE ?= $(HOME)/go/pkg/mod
# Workspace-local GOCACHE for all compile/lint/test paths to avoid stale-build
# artefacts when switching branches or when the global cache becomes inconsistent.
GO_ENV = env GOCACHE=$(CURDIR)/.gocache GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=go1.26.2+auto

build:
	$(GO_ENV) $(GO) build -o bin/agentd ./cmd/agentd

# Scoped runs: make test PKG=./internal/api/controllers/... RUN=TestGateway
PKG ?= $(shell go list ./... | grep -vE '^agentd/(web|docs)$$' | sed 's|^agentd/|./|')
RUN ?=
TEST_FLAGS = -v -race -cover
ifneq ($(strip $(RUN)),)
TEST_FLAGS += -run $(RUN)
endif

test:
	$(GO_ENV) $(GO) test $(TEST_FLAGS) $(PKG)

# E2E tests against live devenv stack. Starts devenv if not already running
# (or reuses it). dev-up brings up every profile, not just `default`, because
# J07/J09/J10 run against the healing, faults/breaker and disk profiles.
# Runs with -tags=e2e and does not run with `make test`.
#
# The full suite is slow: J08 alone waits out a ~2m recovery window, and J10
# holds for several watchdog passes to prove dedup.
#
# -count=1 disables Go's test result cache, which silently masks reruns (e.g.,
# run 2 with `-count=1 removed` would report "ok (cached)" in 0s, hiding a
# real second invocation). This is mandatory when running the suite repeatedly
# to verify flakiness fixes.
test-e2e: dev-up
	$(GO_ENV) $(GO) test -v -race -tags=e2e -count=1 -timeout=1800s ./test/e2e/...

# Compile (but don't run) per-package test binaries, e.g. for a debugger that
# needs a standalone `go test -c` binary. Output is scoped to bin/test/ (git-
# ignored) instead of littering the repo root.
TESTBIN_DIR := bin/test

test-bins:
	@mkdir -p $(TESTBIN_DIR)
	@for pkg in $(PKG); do \
		name=$$(echo $$pkg | sed 's#^\./##; s#/#_#g'); \
		echo "compiling $$pkg -> $(TESTBIN_DIR)/$$name.test"; \
		$(GO_ENV) $(GO) test -c -o $(TESTBIN_DIR)/$$name.test $$pkg || exit 1; \
	done

smoke-contract:
	$(GO_ENV) $(GO) test -count=1 ./internal/gateway/providers -run 'WireContract|TestWireContract_ErrorStatusMapping|TestOpenAIWireContract'
	@echo "Runtime smoke: tools/diag/llm-smoke.sh <base_url> <model> [key]"

coverage:
	$(GO_ENV) $(GO) test -v -race -covermode=atomic -coverpkg=$(COVERPKG) -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -html=coverage.out

run:
	$(GO) run ./cmd/agentd

tidy:
	$(GO) mod tidy

lint:
	$(GO_ENV) $(GOLANGCI_LINT) run $(PKG)

lint-install:
	$(GO) install -v github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

podman-test:
	podman build -f test/container/Dockerfile -t agentd-test .
	podman run --rm agentd-test

# Compose file paths must be absolute: podman-compose 1.3.0 chdirs before
# re-opening the file, so a relative -f fails depending on how it is invoked.
COMPOSE := $(CURDIR)/devenv/compose.yaml

# Every profile must be named in one invocation. podman-compose 1.3.0
# resolves depends_on only within the *activated* profiles, so bringing up a
# single non-default profile alone fails with KeyError: 'litellm'.
COMPOSE_PROFILES := --profile default --profile healing --profile faults \
                     --profile breaker --profile disk --profile tiered

# dev-up blocks until every container with a healthcheck is healthy. podman-compose
# does not reliably honour depends_on service_healthy, and `up -d` returns as soon
# as containers exist; without this wait the first tasks reach litellm while it is
# still refusing connections, which trips the circuit breaker for its full timeout.
DEV_HEALTH_WAIT ?= 180

# All six agentd services share one prebuilt image (B-007). podman-compose tags a
# separate image per service when each declares build:, so the Go compile ran once
# per service -- six per dev-up. Build the tag once here and let compose only start
# containers. --build is kept for mockllm, which still has its own build context.
# Exported so devenv/compose.yaml resolves ${AGENTD_IMAGE:-agentd:local} to the
# same tag that dev-build just produced. Keep the two in sync.
export AGENTD_IMAGE ?= agentd:local

dev-build:
	podman build -t $(AGENTD_IMAGE) .

# --force-recreate: compose compares the service image by tag, so a rebuilt
# agentd:local would otherwise leave the previous container (and binary) running.
dev-up: dev-build
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) up --build --force-recreate -d
	@echo "waiting up to $(DEV_HEALTH_WAIT)s for containers to become healthy"
	@deadline=$$(( $$(date +%s) + $(DEV_HEALTH_WAIT) )); \
	while :; do \
		pending=$$(podman ps --filter label=io.podman.compose.project --format '{{.Names}} {{.Status}}' | grep -E 'starting|unhealthy' || true); \
		[ -z "$$pending" ] && break; \
		if [ $$(date +%s) -ge $$deadline ]; then echo "not healthy in time:"; echo "$$pending"; exit 1; fi; \
		sleep 2; \
	done

dev-down:
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) down

# dev-clean is like dev-down but also removes all named volumes, forcing a
# clean database and cache on the next dev-up. Use this for repeated test
# runs that need to verify fixes; plain dev-down keeps volumes intact so
# state persists across restarts (useful for manual testing).
dev-clean:
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) down -v

dev-logs:
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) logs -f

loc:
	$(GO) run ./tools/checkloc --max-lines 300

minfunc:
	$(GO) run ./tools/checkminfunc --min-lines 3

# Accept current set of short functions into the baseline (use when a short
# function is intentional, e.g. interface method or trivial helper).
minfunc-accept:
	$(GO) run ./tools/checkminfunc --min-lines 3 --update-baseline

# Docs linting. Dependencies: `npm install` (markdownlint-cli2 via the root
# package.json script) and `lychee` (single binary, see .lychee.toml). These
# cover docs/** plus root-level guides. The file set for Markdown linting lives
# in package.json's `lint:md` script (single source of truth), invoked here.
LYCHEE ?= lychee

lint-md:
	npm run lint:md

lint-links:
	$(LYCHEE) --config .lychee.toml README.md docs CONTRIBUTING.md GUARDRAILS.md REVIEW.md STYLEGUIDE.md

lint-docs: lint-md lint-links

# Python unit tests for the devenv mock LLM's scenario dispatch and tiered
# replies (devenv/mockllm/test_server.py). No third-party deps: stdlib unittest.
test-mockllm:
	python3 -m unittest discover -s devenv/mockllm -p 'test_server.py'

# check is the required merge gate: loc + minfunc + lint + test + lint-docs
# (Markdown and link checks) + the mock LLM scenario tests. Run before pushing.
check: loc minfunc lint test test-mockllm lint-docs
