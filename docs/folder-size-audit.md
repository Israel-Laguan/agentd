# Folder Size Audit

Primary ranking uses non-test Go files. Totals include Go tests and other files.

Thresholds: high >= 10, low >= 5

## Folders With 10+ Non-Test Go Files

- 30 non-test Go, 81 tests, 0 other, 111 total: `internal/queue/worker`
- 22 non-test Go, 22 tests, 0 other, 44 total: `internal/config`
- 22 non-test Go, 31 tests, 0 other, 53 total: `internal/kanban`
- 21 non-test Go, 43 tests, 0 other, 64 total: `internal/queue`
- 17 non-test Go, 20 tests, 0 other, 37 total: `cmd/agentd`
- 14 non-test Go, 0 tests, 1 other, 15 total: `internal/kanban/db`
- 13 non-test Go, 9 tests, 0 other, 22 total: `internal/agent/context`
- 12 non-test Go, 8 tests, 0 other, 20 total: `internal/models`
- 12 non-test Go, 6 tests, 0 other, 18 total: `internal/testutil`
- 11 non-test Go, 0 tests, 0 other, 11 total: `internal/agent/hooks`
- 11 non-test Go, 6 tests, 1 other, 18 total: `internal/agent/runtime`
- 11 non-test Go, 3 tests, 0 other, 14 total: `internal/agent/tools`
- 10 non-test Go, 11 tests, 0 other, 21 total: `internal/api/controllers`
- 10 non-test Go, 10 tests, 0 other, 20 total: `internal/gateway/truncation`
- 10 non-test Go, 8 tests, 0 other, 18 total: `internal/sandbox`

## Folders With 5-9 Non-Test Go Files

- 8 non-test Go, 11 tests, 0 other, 19 total: `internal/gateway/routing`
- 8 non-test Go, 9 tests, 0 other, 17 total: `internal/kanban/migrations`
- 7 non-test Go, 6 tests, 0 other, 13 total: `internal/capabilities/plugin`
- 7 non-test Go, 12 tests, 0 other, 19 total: `internal/gateway/providers`
- 7 non-test Go, 12 tests, 0 other, 19 total: `internal/memory`
- 7 non-test Go, 6 tests, 0 other, 13 total: `internal/queue/safety`
- 7 non-test Go, 8 tests, 0 other, 15 total: `internal/queue/worker/agentic`
- 6 non-test Go, 4 tests, 0 other, 10 total: `internal/bus`
- 6 non-test Go, 8 tests, 0 other, 14 total: `internal/frontdesk`
- 6 non-test Go, 9 tests, 0 other, 15 total: `internal/services`
- 5 non-test Go, 2 tests, 0 other, 7 total: `internal/agent/filecontext`

## Test-Heavy Folders With 10+ Go Test Files

- 81 Go tests, 30 non-test Go, 0 other, 111 total: `internal/queue/worker`
- 43 Go tests, 21 non-test Go, 0 other, 64 total: `internal/queue`
- 31 Go tests, 22 non-test Go, 0 other, 53 total: `internal/kanban`
- 22 Go tests, 22 non-test Go, 0 other, 44 total: `internal/config`
- 20 Go tests, 17 non-test Go, 0 other, 37 total: `cmd/agentd`
- 19 Go tests, 2 non-test Go, 0 other, 21 total: `internal/gateway`
- 14 Go tests, 0 non-test Go, 0 other, 14 total: `internal/api/tests/feature`
- 12 Go tests, 7 non-test Go, 0 other, 19 total: `internal/gateway/providers`
- 12 Go tests, 7 non-test Go, 0 other, 19 total: `internal/memory`
- 11 Go tests, 10 non-test Go, 0 other, 21 total: `internal/api/controllers`
- 11 Go tests, 8 non-test Go, 0 other, 19 total: `internal/gateway/routing`
- 10 Go tests, 10 non-test Go, 0 other, 20 total: `internal/gateway/truncation`
