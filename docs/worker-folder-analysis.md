# Analysis: internal/queue/worker (128 files)

## Current State

| Metric | Value |
|--------|-------|
| Total files | 128 |
| Non-test Go files | 40 |
| Test files | 88 |
| Total lines | ~25,180 |
| Avg lines/file (non-test) | ~265 |

## Observations

### 1. File Size is Manageable
Most non-test files are under 300 lines. No file exceeds 300 lines, which is acceptable. The issue is **quantity**, not size.

### 2. Two Distinct Themes
Files cluster around two responsibilities:
- **Worker orchestration**: `worker.go`, `worker_batch.go`, `worker_legacy.go`, `worker_retry.go`, etc.
- **Agentic/agentic mode**: `worker_agentic*.go`, `worker_agentic_handlers.go`, `worker_agentic_iteration.go`, etc.

### 3. Already-Moved Code
`internal/agent/` already contains duplicated responsibilities:
- `runtime/` → has `ModelRouter`, `CapabilityRouter`, `TopicGuard`, `AuditLogger`
- `hooks/` → has full hook implementations
- `tools/` → has `ToolExecutor`, `ToolManifest`, `RetryConfig`
- `context/` → has `ContextManager`, `MessageEditor`, goals/tracking

### 4. The Aliases Problem (RESOLVED)
`aliases.go` removed. Files now import `internal/agent/*` directly via named imports (`agentcontext`, `agenthooks`, `agentruntime`, `agenttools`, etc.).

### 5. Test Files Dominate
88 test files (~69%) suggests heavy unit testing but also that many small modules exist (one test per source file pattern).

## Recommendations

### Option A: Extract Agentic Mode to Subpackage (Recommended)

Move agentic-specific files to `internal/queue/worker/agentic/`:

```text
worker/agentic/
  ├── agentic.go          # processAgentic + main logic
  ├── handlers.go         # worker_agentic_handlers.go
  ├── iteration.go        # worker_agentic_iteration.go  
  ├── setup.go            # worker_agentic_setup.go
  ├── rewind.go           # worker_agentic_rewind.go
  ├── planning.go         # worker_agentic_planning_test.go (or move to test)
  └── integration_test.go # worker_agentic_integration_test.go
```

**Rationale**: Agentic mode is conceptually a separate execution path. Isolating it reduces cognitive load when working on legacy mode.

### Option B: Merge Small Related Files

Some files could be combined:

| Merge From | Merge Into | Rationale |
|------------|------------|-----------|
| `phase_splitter.go` | `worker_batch.go` | Only used by batch processing |
| `clarification.go` | `worker_elicitation.go` | Both deal with user interaction |
| `message_editor_respec.go` | `worker_messages.go` | Both are message utilities |
| `targeted_redo.go` | `loop_result.go` | Both deal with result handling |
| `worker_elicitation.go` + `elicitor.go` | `elicitation.go` | Duplicate "elicitation" prefix |

### Option C: Remove Aliases Layer

Delete `aliases.go` and have `worker/*.go` import `internal/agent/*` directly. This:
- Reduces ~295 lines of boilerplate
- Makes dependencies explicit
- Eliminates the sync burden when `internal/agent` types change

**Migration**: Replace `worker.AliasType` → `agentcontext.AliasType` via find-replace across ~15 files.

### Option D: Move to internal/agent

Move entire `worker/` to `internal/agent/execution/` or similar:

```text
internal/agent/
  ├── context/     (existing)
  ├── hooks/       (existing)
  ├── runtime/     (existing)
  ├── tools/       (existing)
  ├── execution/   (NEW - move worker/ here)
  │   ├── worker.go
  │   ├── batch.go
  │   ├── legacy.go
  │   ├── agentic.go
  │   └── ...
  └── worker.go    (delete - redirect to execution/)
```

**Rationale**: Aligns with where code already lives (`internal/agent` has context, hooks, tools, runtime). Worker is the "execution engine" for those components.

**Cost**: High — many import paths change, git history fragment.

## Suggested Priority

| Priority | Action | Effort | Status |
|----------|--------|--------|--------|
| 1 | Delete `aliases.go`, fix imports | Medium | DONE - now imports `internal/agent/*` directly |
| 2 | Extract `agentic/` subpackage | Low | IMPLEMENTED - moved agentic execution into `internal/queue/worker/agentic/` |
| 3 | Merge `phase_splitter.go` → `worker_batch.go` | Low | SKIPPED - serves specific purpose in agentic iteration flows |
| 4 | Consider moving to `internal/agent/execution/` | High | DEFERRED - too many import path changes |

## Execution Results

**Analysis findings after implementation:**

1. **Aliases layer removal** - DONE. Files now use direct imports from `internal/agent/*`.

2. **Agentic subpackage extraction** - IMPLEMENTED. Agentic execution now lives in `internal/queue/worker/agentic/`, including `engine.go`, `agentic.go`, `handlers.go`, `iteration.go`, `setup.go`, `rewind.go`, and `session.go`.

3. **Small file merges** - Not worthwhile. Files like `phase_splitter.go` and `message_editor_respec.go` serve distinct purposes in the agentic iteration flow. Merging would reduce clarity.

## Conclusion

The worker package is well-structured as-is:
- Average file size of ~265 lines is acceptable
- Core worker orchestration and agentic execution are separated across the worker package and `internal/queue/worker/agentic/`
- The aliases layer has been removed, and files import `internal/agent/*` dependencies directly

## Files by Category (Current Structure)

**Core Worker:**
- `worker.go` (180 lines)
- `worker_construct.go` (240 lines)
- `worker_support.go` (252 lines)

**Batch Processing:**
- `worker_batch.go` (199 lines)
- `worker_batch_process.go` (188 lines)
- `batcher.go` (188 lines)
- `phase_splitter.go` (183 lines)

**Legacy Mode:**
- `worker_legacy.go` (221 lines)
- `worker_legacy_run.go` (162 lines)

**Agentic Mode:**
- `worker_agentic.go` (140 lines)
- `worker_agentic_*.go` (12 files)

**User Interaction:**
- `worker_elicitation.go` (145 lines)
- `elicitor.go` (212 lines)
- `clarification.go` (129 lines)

**Hooks & Tools:**
- `hooks.go`, `hooks_approval.go`
- `worker_tools.go`
