# agentd Reference

See [`architecture.md`](architecture.md) for system node diagrams, data flows, and architectural invariants.

## Feature Catalog

### Journey 1: Frontdesk Intake

| Feature | Description | Feature File |
| --- | --- | --- |
| Scope clarification | Multi-project requests are detected before planning and returned as a `scope_clarification` payload so the client can approve one scope at a time. | [`openai_intake.feature`](../internal/api/features/openai_intake.feature) |
| Intent classification | Chat is routed through a lightweight classifier. `status_check` requests return a database-backed summary, `plan_request` enters the planning flow, and `ambiguous` returns a clarification payload. | [`openai_intake.feature`](../internal/api/features/openai_intake.feature) |
| Big-file handling | Oversized user content is stashed to disk via `FileStash`. Chat intake sends path references through classification, then reads truncated contents only for planning. | [`chat_file_handling.feature`](../internal/api/features/chat_file_handling.feature) |
| Timeout reply | AI-core timeout failures are intercepted and returned as HTTP 200 with a deterministic `[SYSTEM]` assistant message instead of an error response. | [`chat_timeout.feature`](../internal/api/features/chat_timeout.feature) |

### Journey 2: Task Cycle, Cron, Permissions

| Feature | Description | Feature File |
| --- | --- | --- |
| User-editable cron | The daemon reads `<home>/agentd.crontab` for background job schedules. Standard 5-field entries and `@every <duration>` sub-minute entries are supported. | [`cron_schedule.feature`](../cmd/agentd/features/cron_schedule.feature) |
| Prompt recovery | Timed-out sandbox executions are scanned for interactive prompt patterns. Allowlisted commands get a one-shot non-interactive retry; unrecoverable prompts create a HUMAN task. | [`prompt_recovery.feature`](../internal/queue/features/prompt_recovery.feature) |
| Permission detection | `sudo` is blocked before execution. Failed commands with permission-like output create a HUMAN child task for the privileged host action. | [`permission_detection.feature`](../internal/queue/features/permission_detection.feature) |
| Worker self-breakdown | Workers can return `{too_complex: true, subtasks: [...]}`. The parent moves to `BLOCKED`, child tasks are created, and the parent returns to `READY` after all children complete. | [`task_dependencies.feature`](../internal/kanban/features/task_dependencies.feature) |

### Journey 3: System Resilience and Hardware

| Feature | Description | Feature File |
| --- | --- | --- |
| Network outage handoff | When the LLM circuit breaker stays `OPEN` beyond a threshold, a de-duplicated HUMAN diagnostic task is created under `_system`. | [`outage_handoff.feature`](../internal/queue/features/outage_handoff.feature) |
| Reboot recovery | Boot reconciliation records a global memory and creates a HUMAN review task explaining that interrupted tasks were caused by daemon restart, not task failure. | [`ghost_reconciliation.feature`](../internal/queue/features/ghost_reconciliation.feature) |
| Disk-space watchdog | A cron-driven loop creates a de-duplicated HUMAN task when free disk space falls below the configured threshold. | [`disk_watchdog.feature`](../internal/queue/features/disk_watchdog.feature) |
| Sandbox hardening | Sandbox output is capped and redacted, command paths are jailed, env vars are allow-listed, and Unix guardrails apply kill grace and resource limits. | [`sandbox_hardening.feature`](../internal/sandbox/features/sandbox_hardening.feature) |
| Task deadline reaper | Each dispatched worker goroutine runs under a `queue.task_deadline` wall-clock timeout. Stuck workers are cancelled and their semaphore slots released. | [`task_deadline.feature`](../internal/queue/features/task_deadline.feature) |
| Adaptive backoff polling | The dispatch loop doubles its polling interval when no tasks are found, capped at `queue.poll_max_interval`, and resets to the base interval on a successful claim. | [`adaptive_backoff.feature`](../internal/queue/features/adaptive_backoff.feature) |
| Dispatch panic safety | An outer `recover` in each dispatch goroutine prevents worker panics from crashing the daemon and guarantees the semaphore slot is released. | [`dispatch_panic_safety.feature`](../internal/queue/features/dispatch_panic_safety.feature) |

### Journey 4: Human Loop and Automation

| Feature | Description | Feature File |
| --- | --- | --- |
| Self-healing retry | Worker retries run through a deterministic healing ladder (lower temperature, increase context, compress, upgrade model, force split) before human handoff. | [`self_healing.feature`](../internal/queue/features/self_healing.feature) |
| AI Horde provider | Provider fallback can cascade through OpenAI, Ollama, and opt-in AI Horde before creating a HUMAN task on exhaustion. | [`ai_gateway_resilience_fallback.feature`](../internal/gateway/features/ai_gateway_resilience_fallback.feature) |
| Config flag | `agentd --config <file>` loads an explicit YAML config that overrides environment variables for keys present in the file. | [`config_flag.feature`](../cmd/agentd/features/config_flag.feature) |
| Phase planning cap | Plans are capped at `gateway.max_tasks_per_phase` tasks. Oversized plans are trimmed with a `Plan Phase N` continuation task. | [`phase_planning.feature`](../internal/queue/features/phase_planning.feature) |
| Phase cap enforcement (gateway) | The gateway deterministically trims oversized LLM plans to the configured cap and folds remaining tasks into a continuation task. | [`plan_phase_cap.feature`](../internal/gateway/features/plan_phase_cap.feature) |
| Truncation policies | Gateway truncation supports `middle_out`, `head_tail`, `summarize`, and `reject` policies with per-provider input budgets. | [`truncation_strategies.feature`](../internal/gateway/features/truncation_strategies.feature) |
| Heartbeat reconciliation | A periodic loop reconciles `RUNNING` tasks against OS PIDs and heartbeat timestamps, resetting stale tasks to `READY`. | [`heartbeat_reconciliation.feature`](../internal/queue/features/heartbeat_reconciliation.feature) |
| Librarian archival | Completed task logs are archived to tar.gz, summarized via map-reduce into durable memories, and cleaned after a grace period. | [`librarian_archival.feature`](../internal/memory/features/librarian_archival.feature) |
| Memory recall | FTS5-based recall pre-fetches relevant memories before LLM calls with namespace isolation and 500ms hard timeout. | [`recall_namespace.feature`](../internal/memory/features/recall_namespace.feature), [`recall_timeout.feature`](../internal/memory/features/recall_timeout.feature) |
| Memory dreaming | Nightly cron consolidates redundant memories via LLM merge and supersedes originals. | [`dream_consolidation.feature`](../internal/memory/features/dream_consolidation.feature) |
| User preferences | Human preferences are stored as `USER_PREFERENCE`-scoped memories and injected into chat prompts. | [`user_preferences.feature`](../internal/memory/features/user_preferences.feature) |
| Distillation discard | Empty or junk memory extractions are discarded rather than stored. | [`distillation_discard.feature`](../internal/memory/features/distillation_discard.feature) |

### Journey 5: Dynamic Workforce Manager

| Feature | Description |
| --- | --- |
| OpenAI tool calls | When a client lists `tools` on `POST /v1/chat/completions`, plan and status responses are returned as `create_plan` / `status_report` tool calls in addition to the JSON content. Wire compat is verified by round-tripping our response through the official `github.com/openai/openai-go/v3` types. |
| Streaming chat | `stream:true` switches the response to `text/event-stream` and emits `chat.completion.chunk` SSE frames terminated by `data: [DONE]`. |
| Agent registry | `GET/POST/PATCH/DELETE /api/v1/agents` manage `AgentProfile` rows. The `default` profile is protected from deletion, and any profile referenced by an existing task is refused on `DELETE` with 409 `STATE_CONFLICT`. |
| Manager loop | `POST /api/v1/tasks/{id}/assign` retargets to a different agent, `POST /api/v1/tasks/{id}/split` blocks the parent and creates ready subtasks, and `POST /api/v1/tasks/{id}/retry` returns a stuck task to `READY`. Reassignment of a `RUNNING` task is refused with 409. |
| Embedded agent snapshot | Task list/get/assign/retry responses inline the `AgentProfile` snapshot under `agent` so the cockpit avoids an extra round-trip per task. |
| Worker honours profile | Workers forward `profile.Provider`, `profile.Model`, `profile.MaxTokens`, and `profile.Temperature` into `gateway.AIRequest`. The router precedence is: explicit profile values > role routing > first configured provider. |
| Live cockpit | The SSE stream emits named `event:` lines (`task_failed`, `task_updated`, `log_chunk`, `agent_updated`, `agent_deleted`, `task_assigned`, `task_split`, `task_retried`, …) and accepts `?task_id=` or `?project_id=` to narrow the topic subscription. |

## Task States

| State | Meaning |
| --- | --- |
| `PENDING` | Waiting for prerequisite tasks to complete. |
| `READY` | All dependencies met; eligible for worker claim. |
| `QUEUED` | Claimed by the daemon semaphore; waiting for a worker slot. |
| `RUNNING` | A worker is actively executing the task. |
| `BLOCKED` | Parent task paused until child sub-tasks or human work complete. |
| `COMPLETED` | Task finished successfully. |
| `FAILED` | Task exhausted retries or was evicted. |
| `FAILED_REQUIRES_HUMAN` | Task evicted after max retries; human review required before retry. |
| `IN_CONSIDERATION` | Human comment interrupted the task; awaiting re-evaluation. |

## Event Types

| Event Type | Source | Meaning |
| --- | --- | --- |
| `PROMPT_DETECTED` | Worker | Interactive prompt detected in sandbox output after a timeout. |
| `PROMPT_HANDOFF` | Worker | HUMAN task created for an unrecoverable interactive prompt. |
| `PERMISSION_HANDOFF` | Worker | Privileged work detected; HUMAN task created. |
| `SANDBOX_VIOLATION` | Sandbox | Command blocked before execution due to sandbox policy (e.g., `sudo`, path escape). |
| `LOG_CHUNK` | Sandbox | Streaming stdout/stderr line emitted during command execution (after scrubber redaction). |
| `TASK_BREAKDOWN` | Worker | Oversized task split into child tasks; parent blocked. |
| `LLM_OUTAGE_HANDOFF` | Daemon | HUMAN task created for AI provider outage diagnostics. |
| `REBOOT_RECOVERY_HANDOFF` | Daemon | HUMAN review task created after resetting interrupted tasks on startup. |
| `DISK_SPACE_CRITICAL` | Watchdog | Free disk space below configured threshold. |
| `TUNE` | Worker | Model parameters changed for a retry attempt. |
| `HEALING_SPLIT` | Worker | Repeated-failure task forced into the breakdown path. |
| `HEALING_HANDOFF` | Worker | Self-healing exhausted; HUMAN review task created. |
| `POISON_PILL_HANDOFF` | Worker | Task evicted after max retries; moved to FAILED_REQUIRES_HUMAN. |
| `PROVIDER_EXHAUSTED_HANDOFF` | Worker | All configured AI providers exhausted; HUMAN review task created. |
| `PHASE_PLANNING` | Worker | Phase-planning task completed; next batch of project tasks appended. |
| `HEARTBEAT_RECONCILE` | Daemon | Running tasks reconciled against heartbeat and OS PID state. |
| `LOG_ARCHIVED` | Librarian | Raw task logs archived before memory ingestion. |
| `MEMORY_INGESTED` | Librarian | Durable memory summary recorded for completed task logs. |
| `MEMORY_DISCARDED` | Librarian | Memory extraction was empty or junk; discarded without recording. |
| `EVENTS_PURGED` | Librarian | Curated events deleted after archive grace period expired. |

## Config Keys

| Key | Default | Meaning |
| --- | --- | --- |
| `gateway.truncation.strategy` | `head_tail` | Active truncation algorithm (`head_tail` or `middle_out`). |
| `gateway.truncation.head_ratio` | `0.5` | Head/tail budget portion allocated to the beginning of content. |
| `gateway.truncation.max_input_chars` | _(provider-dependent)_ | Default per-message character budget for gateway truncation. |
| `gateway.truncation.stash_threshold` | _(configured)_ | Character threshold above which content is saved to the file stash. |
| `uploads_dir` | `<home>/uploads` | Directory for oversized uploaded or referenced file content. |
| `breaker.handoff_after` | `2m` | Duration the LLM breaker must stay open before creating a HUMAN outage task. |
| `disk.free_threshold_percent` | `10.0` | Minimum acceptable free disk percentage. |
| `healing.enabled` | `true` | Enables self-healing retry parameter tuning. |
| `healing.strategy` | `increase_effort` | Built-in healing ladder (`increase_effort` or `minimize_variables`). |
| `healing.steps` | _(empty)_ | Optional custom healing step list overriding the preset ladder. |
| `healing.max_adjustments` | `0` | Maximum healing steps before HUMAN handoff; `0` means full ladder. |
| `healing.upgrade_model` | _(empty)_ | Model override used by the upgrade healing step. |
| `healing.upgrade_provider` | _(empty)_ | Provider override used by the upgrade healing step. |
| `healing.context_multiplier` | `1.5` | Multiplier for increasing retry context budget. |
| `gateway.horde.base_url` | `https://aihorde.net/api` | AI Horde endpoint URL. |
| `gateway.horde.api_key` | `0000000000` | Optional AI Horde API key (anonymous default). |
| `gateway.horde.model` | _(empty)_ | AI Horde model selection. |
| `gateway.horde.timeout` | `5m` | Maximum wait time for an AI Horde queued generation. |
| `gateway.horde.poll_interval` | `4s` | Delay between AI Horde status checks. |
| `gateway.max_tasks_per_phase` | `7` | Maximum tasks Frontdesk may emit for a single planning phase. |
| `gateway.truncator.policy` | `middle_out` | Default truncation policy for outbound LLM requests. |
| `gateway.truncator.max_input_chars` | _(configured)_ | Default input character budget for the truncator. |
| `queue.task_deadline` | `10m` | Wall-clock timeout per dispatched worker task (the Reaper). |
| `queue.queued_reconcile_after` | `10m` | Minimum age of a QUEUED claim before orphan recovery resets it to READY. Independent of `task_deadline`. Set to `0` to disable. |
| `queue.poll_max_interval` | `10s` | Adaptive backoff ceiling when no tasks are available. |
| `sandbox.inactivity_timeout` | `60s` | Max stdout/stderr silence before sandbox timeout triggers. |
| `sandbox.wall_timeout` | `10m` | Max wall-clock execution time for each sandbox command payload. |
| `sandbox.kill_grace` | `2s` | Grace window between SIGTERM and SIGKILL for timed-out process groups. |
| `sandbox.max_log_bytes` | `5242880` | Per-stream cap for final command output buffers with head/tail truncation marker. |
| `sandbox.env_allowlist` | `PATH,HOME,LANG,LC_ALL,USER` | Parent environment keys allowed into sandbox payload env vars. |
| `sandbox.extra_env` | `CI=true,DEBIAN_FRONTEND=noninteractive,NO_COLOR=1` | Forced non-interactive environment variables appended to payload env vars. |
| `sandbox.scrub_patterns` | `[]` | Extra regex patterns used by sandbox log scrubber before persistence. |
| `sandbox.limits.address_space_bytes` | `2147483648` | Requested virtual memory cap applied via Unix `ulimit -Sv` guard. |
| `sandbox.limits.cpu_seconds` | `600` | Requested CPU-time cap applied via Unix `ulimit -t` guard. |
| `sandbox.limits.open_files` | `1024` | Requested open-file-descriptor cap applied via Unix `ulimit -n` guard. |
| `sandbox.limits.processes` | `256` | Requested process-count cap applied via Unix `ulimit -u` guard. |
| `heartbeat.stale_after` | `2m` | Maximum age of a running task heartbeat before reset. |
| `librarian.retention_hours` | `24` | Age threshold before completed task logs are eligible for curation. |
| `librarian.archive_grace_days` | `30` | Days to retain archives after memory ingestion. |
| `librarian.chunk_chars` | _(configured)_ | Maximum character size of each map-phase summarization chunk. |
| `librarian.max_reduce_passes` | _(configured)_ | Maximum recursive reduce rounds before concatenating chunk summaries. |
| `librarian.fallback_head_tail_chars` | _(configured)_ | Characters to keep from head and tail in deterministic fallback extraction. |
| `librarian.recall_timeout` | `500ms` | Hard timeout for memory recall queries (Danger B). |
| `librarian.recall_top_k` | `5` | Maximum memories returned per recall query. |
| `librarian.preferences_top_k` | `3` | Maximum user preferences returned per recall. |
| `librarian.dream_cluster_min_size` | `3` | Minimum cluster size for dream consolidation. |
| `librarian.dream_similarity_threshold` | `0.7` | Jaccard similarity threshold for memory clustering. |
