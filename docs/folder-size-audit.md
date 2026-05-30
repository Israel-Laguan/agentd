# Folder Size Audit

Thresholds: high >= 10, low >= 5

## Phase 1 Cleanup Progress

`internal/queue/worker` (originally 210 files) had three subpackages extracted on branch `feat/cleanup-phase-2`:

| Subpackage | Package name | Files | Status |
|---|---|---|---|
| `worker/skills/` | `wskills` | 3 | ✅ complete |
| `worker/filecontext/` | `wfilecontext` | 7 | ✅ complete |
| `worker/session/` | `wsession` | 4 | ✅ complete |
| `worker/` (root) | `worker` | 196 | still large |

Deeper extraction (subagent, hooks, tools, msgctx, etc.) was blocked by Go's same-package receiver constraint and heavy cross-dependencies on unexported root types (`ToolStatus`, `toolName*` constants, `ToolExecutor`, `Worker).

## Folders With 10+ Files

- 195: `internal/queue/worker`
- 64: `internal/queue`
- 58: `internal/config`
- 53: `internal/kanban`
- 37: `cmd/agentd`
- 31: `tasks/done`
- 21: `internal/api/controllers`
- 21: `internal/gateway`
- 20: `internal/gateway/truncation`
- 20: `internal/models`
- 19: `.`
- 19: `internal/gateway/providers`
- 19: `internal/gateway/routing`
- 19: `internal/memory`
- 18: `docs`
- 18: `internal/queue/features`
- 18: `internal/sandbox`
- 17: `internal/kanban/migrations`
- 17: `internal/testutil`
- 17: `web`
- 17: `web/lib`
- 15: `internal/api`
- 15: `internal/kanban/db`
- 15: `internal/services`
- 14: `internal/frontdesk`
- 13: `internal/capabilities/plugin`
- 13: `internal/queue/safety`
- 11: `internal/gateway/features`
- 10: `internal/bus`
- 10: `web/app/components/chat`

## Folders With 5-9 Files

- 9: `tasks`
- 7: `internal/api/server`
- 7: `web/lib/mocks`
- 6: `internal/api/features`
- 6: `internal/memory/features`
- 6: `scripts`
- 5: `internal/api/sse`
- 5: `internal/kanban/features`
