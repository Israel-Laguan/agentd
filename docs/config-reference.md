# Configuration Reference (Extended)

Copy-paste YAML template: [`config.reference.yaml`](../config.reference.yaml).

Key defaults and semantics: [`reference.md`](reference.md#config-keys).

Precedence (highest wins): CLI flags → explicit `--config` file → `AGENTD_*` env → `<home>/config.yaml` → compiled defaults.

Repo or home `.env` files are loaded into `AGENTD_*` at startup (they do not export variables to your shell). When a merged env value overrides the same key in `config.yaml`, agentd emits `config: key overridden by env` at INFO during `Load` (for example when `AGENTD_GATEWAY_ORDER` in `.env` replaces `gateway.order` in `~/.agentd/config.yaml`). Run `agentd config show` to print resolved gateway-related keys with source annotations.

## Filesystem layout

Effective home is resolved from `--home`, `AGENTD_HOME`, or `.env` (CWD then `~/.agentd/.env`) before `config.yaml` is read. The `home:` key in the template is informational only and cannot override that resolved path.

## API

When `api.materialize_token` is non-empty, `POST /api/v1/projects/materialize` (and `workspace/ready`, `human-resolution`) requires header `X-Agentd-Materialize-Token` with that exact value. The CLI `agentd ask` sends it automatically. Requests whose `Origin` is `api.ui_origin` skip the token.

`api.ui_origin` (default `http://localhost:3000`) is the web UI's origin. The daemon sends CORS headers only to it, accepts browser writes only from it, and lets those writes skip the materialize token. Set it to the exact `scheme://host:port` the UI is served from; `""` trusts no browser origin.

`api.source_root` (default: your home directory, `~` is expanded) limits `source_path` on materialize. The path, after following symlinks, must be under this root, and must not be the agentd home, inside it, or a parent of it. Otherwise materialize returns `400`. `""` refuses every `source_path`.

`api.allowed_hosts` (list, default empty) names extra `Host` header values the API accepts. Loopback names (`localhost`, `127.0.0.1`, `::1`) always pass; any other `Host` gets `403`. Add an entry when clients reach the daemon under another name, for example `agentd` for a compose service name. An entry without a port matches any port; `agentd:8765` matches only that port. Env: `AGENTD_API_ALLOWED_HOSTS` (space-separated). See the write guard in [`api-testing.md`](api-testing.md#write-guard).

## Gateway

Tool/JSON fallback behavior is documented in [`provider-tool-calling.md`](provider-tool-calling.md).

### Custom provider registry

The `gateway.providers` list registers one or more provider entries. Each entry must set at least
one of `name` or `adapter` (non-empty after trimming whitespace); otherwise config load fails.
`name` is the operator-facing identifier referenced by `gateway.order` (defaults to the entry's
`adapter` value when omitted); `adapter` selects the Go backend implementation.

Multiple entries may share the same `adapter` at different `base_url` values — each becomes an
independent backend. This enables cascading two OpenAI-compatible endpoints, which is impossible
with the legacy per-vendor slot schema:

```yaml
gateway:
  providers:
    - name: openai
      adapter: openai
      base_url: "https://api.openai.com/v1"
      api_key_env: OPENAI_API_KEY
      model: "gpt-4o-mini"
    - name: poolside
      adapter: openai
      base_url: "https://inference.poolside.ai/v1"
      api_key_env: POOLSIDE_API_KEY
      model: "poolside/laguna-m.1"
      capabilities:
        chat_tools: true
  order: [openai, poolside, anthropic]
```

Adapter-specific settings belong in `options` (each adapter reads its own keys; unknown keys
log a warning and are ignored):

```yaml
gateway:
  providers:
  - name: horde
    adapter: horde
    base_url: "https://aihorde.net/api"
    options:
      poll_interval: "5s"
  order: [openai, horde]
```

Single custom entry (e.g. vLLM or LM Studio alongside the named OpenAI slot):

```yaml
gateway:
  providers:
    - name: vllm
      adapter: openai
      base_url: "http://127.0.0.1:8000/v1"
      model: "your-model-name"
  order: [openai, vllm]
  openai:
    base_url: "https://api.openai.com/v1"
    api_key_env: OPENAI_API_KEY
```

Legacy flat keys (`gateway.openai.*`, `gateway.anthropic.*`, etc.) are still supported and produce
an implicit entry with `name` matching the vendor (e.g. `name: openai, adapter: openai`). Existing
deployments require no config changes.

All fields per entry:

| Field | Notes |
| --- | --- |
| `name` | Optional; defaults to the entry's `adapter` value when omitted. Must be unique after defaulting — duplicate names (explicit or implicit) are rejected at config load. Referenced by `gateway.order` and `req.Provider`; use distinct names when running multiple entries with the same adapter (e.g. `openai` + `poolside`, both `adapter: openai`). |
| `adapter` | Optional; defaults to the entry's `name` when omitted. Backend implementation: `openai`, `anthropic`, `ollama`, `llamacpp`, `horde`. Legacy configs may use `adapter: gemini`; canonicalization remaps `adapter: gemini` to `openai` regardless of `name`. Prefer explicit `name: gemini` with `adapter: openai`. |
| `base_url` | Provider endpoint URL. |
| `model` | Default model for this entry. |
| `api_key_env` | Env var name to read the API key from at startup. |
| `api_key` | Inline API key (prefer `api_key_env`). |
| `health` | Health-check variant: `api_key`, `ollama`, `llamacpp`, `horde`, or empty (adapter default). |
| `capabilities.chat_tools` | Override adapter default for chat tool support (`true`/`false`). |
| `options` | Adapter-specific key/value map. Each adapter reads only its known keys; unrecognized keys log a warning at startup and are ignored. Example: Horde `poll_interval` (duration string). Invalid typed values for known keys fall back to adapter defaults with an error log. |

### Provider blocks

- **openai / anthropic / gemini**: set `api_key` in YAML or via `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `GEMINI_API_KEY`.

#### `gateway.gemini`

Built-in slot for Google's OpenAI-compatible Gemini endpoint (uses the `openai` adapter internally):

| Key | Default | Notes |
| --- | --- | --- |
| `gateway.gemini.base_url` | `https://generativelanguage.googleapis.com/v1beta/openai` | OpenAI-compatible base URL |
| `gateway.gemini.api_key` | `""` | Inline key; prefer `GEMINI_API_KEY` in `.env` |
| `gateway.gemini.model` | `""` | No built-in default (past ids have gone stale/404); set explicitly |
| `gateway.gemini.max_input_chars` | `0` | `0` inherits `gateway.truncator.max_input_chars` |
| `gateway.gemini.timeout` | `5m` | Per-request HTTP timeout |

Gemini-only quick start (warmup skip, profile PATCH, healing): [README § First Run](../README.md#first-run).

- **ollama / llamacpp**: local inference endpoints; set `model` to match your deployed model.
- **horde**: AI Horde fallback; anonymous key `0000000000` is valid.
- **max_input_chars**: `0` inherits `gateway.truncator.max_input_chars`.
- **warmup_enabled**: set `false` or use `agentd start --skip-llm-warmup` to skip billable startup probe.

### Routing: agents and stages

`agents:` names the model targets a route can send a call to. `stages:` keys a route by purpose (the names in `internal/gateway/spec/purpose.go`, such as `create.elicit` or `exec.work_plan`) and gives it a `default` agent and an ordered list of `rules`. The first rule whose `when` holds picks its `use` agent. With no matching rule, the `default` applies. With neither, the call takes the gateway cascade (`gateway.order`), exactly as it did before routing existed, so a config with neither section changes nothing.

```yaml
agents:
  quick: { provider: gemini, model: gemini-2.5-flash }   # every config agent is L1
  local: { provider: ollama, model: llama3:8b }
stages:
  create.elicit:
    default: quick
  exec.work_plan:
    default: local
    rules:
      - when: { requires_workspace_change: true }
        use: quick
```

- An agent has a `provider` (required), and an optional `model`. Every config agent is a single model call (level `L1`); `level` is not a key, so a config cannot make a model claim L3. A purpose declares the minimum level it needs; a route to an agent below that level fails at load, so `exec.turn` and `overhead.subagent_turn` cannot be routed to a config agent. L3 agents arrive with T-042.
- `when` keys: `requires_workspace_change` (`true` or `false`), which matches tasks that must change the workspace. A rule needs a condition and a `use`.
- An explicit `AgentID` on a task or a tier step is a pin. It wins over every rule and injects no provider or model. The seeded `default` profile is not a pin, so tasks on it follow their stage's route.
- Every decision logs one `routing decision` line with the purpose, the required level, the rule that matched and the agent chosen.
- Unknown agents, unknown keys, bad rules and routes below a purpose's level all fail at config load.

`gateway.role_models` was removed. Setting it fails at load and names `stages` and `agents` as its replacement. The seeded `default` profile uses empty `provider`/`model` and follows stage routes. The other built-in profiles from `agentd init` (such as the `tier-*` step profiles) are `AgentID` pins: with empty `provider`/`model` they take the gateway cascade, and a manual PATCH can give them explicit values, but no stage route applies to them. See [init-startup.md](init-startup.md). A routed non-tool provider is skipped when another candidate supports tools; it gets JSON fallback only when no candidate supports tools.

### Truncation

`truncator.policy`: `head_tail`, `middle_out`, `summarize`, or `reject`; it cuts what is sent to the provider. `truncation.strategy` (`head_tail` or `middle_out`) cuts the agent's own working context, and each subagent's, when it exceeds the `queue.agentic_context` budgets. A task stops with "context character budget exhausted" only when what is never cut (system prompt, task, preferences, summary, corrections) is still over the budget. `truncation.head_ratio` is the share kept from the head wherever `head_tail` applies, in either place. `truncation.stash_threshold` sets the file-stash offload. `truncation.max_input_chars` is not implemented: it is parsed and ignored, so use `truncator.max_input_chars`.

### MCP servers

External tool servers via Model Context Protocol (config key `gateway.mcp_servers`; formerly `gateway.capabilities`):

```yaml
gateway:
  mcp_servers:
    - type: mcp
      name: "github"
      server_url: "https://api.example.com/mcp"
      auth:
        type: bearer
        token: "${MCP_TOKEN}"
```

## Agentic

Every agent profile runs the agentic loop; the API refuses `agentic_mode: false` (see [api-testing.md](api-testing.md)). Keys marked **not implemented** are parsed and ignored (B-065); `config.reference.yaml` carries the same marker and a test fails when the two drift.

| Key | Notes |
| --- | --- |
| `context_warning_threshold` | Fraction (0–1) of zone budget triggering preemptive summarization; `0` disables. Default `0.85`. |
| `tool_failure_streak` | Consecutive errors on the same tool before `LoopToolFailure`; `0` = fatal-only. Separately, and not configurable, the same tool call (name and arguments) returning the same result 3 times in one task run stops the loop with `NO_PROGRESS` and moves the task to `FAILED_REQUIRES_HUMAN` without a retry (B-081). |
| `topic_guard` | Drift detection; opens a fresh in-task session when human input is unrelated. `sensitivity` 0–1 (default `0.5`). |
| `tool_credentials` | Map tool name → env var (injected per call, never in args). See [`reference.md`](reference.md). |
| `approval_gates` | **Not implemented.** Planned: high-stakes tools require human approval via HUMAN subtask before execution. Nothing sets `AgentProfile.GatedTools` yet. |
| `tool_rate_limits` | **Not implemented.** Planned: per-session PreToolUse call caps. The unused rate-limit hook was removed; a future implementation starts from the call seam (S18). |
| `audit` | JSONL log of tool dispatches, turn snapshots, history edits; args stored as SHA-256 hash. |
| `external_tools` | Wrap in `<external_content>`; empty = all non-builtin; list limits scope. |
| `review` | **Not implemented.** Planned: review loop; `AgentProfile.RequireReview` is never set. |
| `skills` | **Not implemented** under `agentic`. Skills are configured under `queue.skills` (`project_dir`, `global_dir`, `threshold`, `top_k`). |
| `file_context` | Smart file context for `read` (convert → cache → embed → select). Requires `pdftotext` (poppler) on PATH; embeddings use first OpenAI-compatible provider. |
| `planning` | Plan→execute mode; `complexity_threshold` `0` disables (e.g. `500` enables). |
| `tool_manifest` | Filter tools by classified task type. Quote wildcard: `code_gen: ["*"]` (unquoted `*` is YAML alias syntax). |
| `scheduler` | Cron/deferred dispatch, checked every `tick_interval` (default `1m`); task fields: `id`, `cron_expr`, `run_after`, `task_type`, `context_fn`, `context_args`, `output_target`, `title`, `description_template`, `project_id`. A task runs in its `project_id`, else `scheduler.project_id`; with neither, config load fails when the scheduler is enabled (B-076): `_system` is not an executable workspace. |
| `batching` | Batch independent tool-free tasks in one LLM call (same project + agent). |

## Healing

| Key | Notes |
| --- | --- |
| `healing.enabled` | Self-healing ladder and provider-exhaustion HUMAN handoffs. When `false`, failures terminate without healing subtasks. |
| `healing.strategy` | Built-in ladder: `increase_effort` or `minimize_variables`. |
| `healing.steps` | Optional custom step list overriding the preset ladder. |
| `healing.max_adjustments` | Max healing steps before HUMAN handoff; `0` = full ladder. |
| `healing.max_healing_tasks` | Max HUMAN handoff subtasks per parent before failing; `0` = unlimited. |
| `healing.outage_handoff_enabled` | When `false`, skip `_system` "System Offline" tasks when the global breaker stays open. |
| `healing.upgrade_model` | Model override for the upgrade healing step. |
| `healing.upgrade_provider` | Provider override for the upgrade healing step. |
| `healing.context_multiplier` | Multiplier for increasing retry context budget. |

## Channel

Dispatch validation defaults apply when absent. Set both `max_message_size` and `rate_limit` to `0` to disable the channel gate entirely.

- **max_message_size**: bytes; `0` = unlimited. Reject payloads above threshold when `> 0`.
- **rate_limit**: max task admissions per task within `rate_window` (queue path keys `session_id` to task ID); `0` = unlimited.
- **rate_window**: sliding window in seconds for per-session rate limiting.

## Librarian

`librarian.*` tunes memory retention, chunking, recall and dream clustering; `config.reference.yaml` lists the keys and defaults. `librarian.preferences_top_k` is not implemented: it is parsed and ignored, because saved preferences are always recalled in full.

## Queue

Cron schedules live in `<home>/agentd.crontab`.

| Key | Notes |
| --- | --- |
| `task_deadline` | Wall-clock timeout per worker task (the Reaper). |
| `queued_reconcile_after` | Min age before resetting stale `QUEUED` claims; independent of `task_deadline`. |
| `token_budget` | Worker-level per-task token cap; `0` = unlimited. `gateway.budget` (`tokens_per_task`) is not implemented (parsed and ignored); use this key. |
| `agentic_character_budget` | `0` inherits `gateway.truncator.max_input_chars`. |
| `hitl.legacy_handoff_timeout` | Prompt/permission/healing/provider handoffs. Approval, review, and clarification use the 30m approval path. |
| `split.max_depth` | Max parent→child split generations before a HUMAN handoff, for the healing split (`0` = no cap; default `2`). Replaces the removed legacy `max_breakdown_depth` key. |
| `split.max_subtasks` | Max subtasks from one split (`0` = no cap; default `5`). Replaces the removed legacy `max_subtasks_per_breakdown` key. |
| `tool_timeouts` | Per-tool dispatch timeouts; `"default"` applies to unlisted tools. Go duration strings (`"30s"`, `"1m"`). |
| `tool_retries` | Transient failures retried with exponential backoff + jitter; model never sees intermediate failures. Only listed tools are eligible. |

Load refuses the removed legacy keys under `queue`: `legacy.max_breakdown_depth`, `legacy.max_subtasks_per_breakdown`, `legacy.preflight_score`, `legacy.reject_score` and `legacy.max_description_len`. The error names the replacement key, or says to delete the key when none exists. Viper alone would ignore them without a word.

## AI usage

| Key | Notes |
| --- | --- |
| `ai_usage.retention_days` | Delete `ai_calls` and `tool_calls` rows older than this many days. `0` (default) keeps them forever; a negative value fails config load. The daemon prunes at start and every 6 hours. See [ai-usage-report.md](ai-usage-report.md). |

## Plugins

> **Not loaded yet (B-063).** The daemon does not mount plugins. The `plugins` block in `config.reference.yaml` (`enabled`, `path`, `shell_hook_timeout`) is not implemented: nothing reads it. The format below is the planned one.

Plugins are directories inside a plugins directory, each containing a `manifest.json` declaring hooks, capabilities, and env requirements. Load order follows numeric directory prefix (`01-security/`, `02-observability/`, `10-code-quality/`) or the manifest `priority` field. A restart is required to pick up new or changed plugins.
