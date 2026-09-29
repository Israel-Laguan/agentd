GO ?= go
GOLANGCI_LINT ?= $(shell $(GO) env GOPATH)/bin/golangci-lint
# Comma-separated patterns for merged coverage (default: entire module). Override to narrow the denominator, e.g. internal-only: $(shell go list ./internal/... | paste -sd, -)
COVERPKG ?= ./...

.PHONY: build test test-e2e test-bins coverage run tidy lint lint-install loc minfunc minfunc-accept check podman-test lint-md lint-links lint-docs smoke-contract dev-up dev-down dev-logs

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
test-e2e: dev-up
	$(GO_ENV) $(GO) test -v -race -tags=e2e -timeout=1800s ./test/e2e/...

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
                     --profile breaker --profile disk

dev-up:
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) up --build -d

dev-down:
	podman compose -f $(COMPOSE) $(COMPOSE_PROFILES) down

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

# check is the required merge gate: loc + minfunc + lint + test + lint-docs
# (Markdown and link checks). Run before pushing.
check: loc minfunc lint test lint-docs
