# Configuration Reference (Extended)

Copy-paste YAML template: [`config.reference.yaml`](../config.reference.yaml).

Key defaults and semantics: [`reference.md`](reference.md#config-keys).

Precedence (highest wins): CLI flags → explicit `--config` file → `AGENTD_*` env → `<home>/config.yaml` → compiled defaults.

## Filesystem layout

Effective home is resolved from `--home`, `AGENTD_HOME`, or `.env` (CWD then `~/.agentd/.env`) before `config.yaml` is read. The `home:` key in the template is informational only and cannot override that resolved path.

## API

When `api.materialize_token` is non-empty, `POST /api/v1/projects/materialize` requires header `X-Agentd-Materialize-Token` with that exact value. The CLI `agentd ask` sends it automatically.

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
| `adapter` | Optional; defaults to the entry's `name` when omitted. Backend implementation: `openai`, `anthropic`, `ollama`, `llamacpp`, `horde`, `gemini`. |
| `base_url` | Provider endpoint URL. |
| `model` | Default model for this entry. |
| `api_key_env` | Env var name to read the API key from at startup. |
| `api_key` | Inline API key (prefer `api_key_env`). |
| `health` | Health-check variant: `api_key`, `ollama`, `llamacpp`, `horde`, or empty (adapter default). |
| `capabilities.chat_tools` | Override adapter default for chat tool support (`true`/`false`). |

### Provider blocks

- **openai / anthropic / gemini**: set `api_key` in YAML or via `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `GEMINI_API_KEY`.
- **ollama / llamacpp**: local inference endpoints; set `model` to match your deployed model.
- **horde**: AI Horde fallback; anonymous key `0000000000` is valid.
- **max_input_chars**: `0` inherits `gateway.truncator.max_input_chars`.
- **warmup_enabled**: set `false` or use `agentd start --skip-llm-warmup` to skip billable startup probe.

### Role models

`gateway.role_models` (chat / worker / memory) wires specialized model routing via `WithRoleRouting`. Empty `provider` uses the default cascade order. Built-in agent profiles from `agentd init` also use empty `provider`/`model` so workers delegate to `gateway.order` unless you PATCH explicit values; see [init-startup.md](init-startup.md). Non-tool providers in the memory role still get JSON fallback when tools are sent.

### Truncation

`truncator.policy`: `head_tail`, `middle_out`, `summarize`, or `reject`. The `truncation` block holds active strategy parameters including `stash_threshold` for file-stash offload.

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

Enabled per agent profile via `AgentProfile.agentic_mode`. Tool rate limits are per-session PreToolUse caps.

| Key | Notes |
| --- | --- |
| `context_warning_threshold` | Fraction (0–1) of zone budget triggering preemptive summarization; `0` disables. Default `0.85`. |
| `tool_failure_streak` | Consecutive errors on the same tool before `LoopToolFailure`; `0` = fatal-only. |
| `topic_guard` | Drift detection; opens a fresh in-task session when human input is unrelated. `sensitivity` 0–1 (default `0.5`). |
| `tool_credentials` | Map tool name → env var (injected per call, never in args). See [`reference.md`](reference.md). |
| `approval_gates` | High-stakes tools require human approval via HUMAN subtask before execution. |
| `audit` | JSONL log of tool dispatches, turn snapshots, history edits; args stored as SHA-256 hash. |
| `external_tools` | Wrap in `<external_content>`; empty = all non-builtin; list limits scope. |
| `review` | Review loop; override per profile via `AgentProfile.RequireReview`. |
| `skills` | Markdown injected when TF-IDF matches task; see also `queue.skills.global_dir`. |
| `file_context` | Smart file context for `read` (convert → cache → embed → select). Requires `pdftotext` (poppler) on PATH; embeddings use first OpenAI-compatible provider. |
| `planning` | Plan→execute mode; `complexity_threshold` `0` disables (e.g. `500` enables). |
| `model_routing` | Complexity routing by keyword score; see [`reference.md`](reference.md). |
| `tool_manifest` | Filter tools by classified task type. Quote wildcard: `code_gen: ["*"]` (unquoted `*` is YAML alias syntax). |
| `prompt_templates_path` | Named prompt templates with typed slots (embedded defaults + optional file override). |
| `scheduler` | Cron/deferred dispatch (minute tick); fields: `id`, `cron_expr`, `task_type`, `context_fn`, `context_args`, `output_target`, `title`. |
| `batching` | Batch independent tool-free tasks in one LLM call (same project + agent). |
| `capability_routing` | Route matching intents to external adapters, bypassing the agentic turn loop. Runs after `model_routing`. |

## Channel

Dispatch validation defaults apply when absent. Set both `max_message_size` and `rate_limit` to `0` to disable the channel gate entirely.

- **max_message_size**: bytes; `0` = unlimited. Reject payloads above threshold when `> 0`.
- **rate_limit**: max task admissions per task within `rate_window` (queue path keys `session_id` to task ID); `0` = unlimited.
- **rate_window**: sliding window in seconds for per-session rate limiting.

## Queue

Cron schedules live in `<home>/agentd.crontab`.

| Key | Notes |
| --- | --- |
| `task_deadline` | Wall-clock timeout per worker task (the Reaper). |
| `queued_reconcile_after` | Min age before resetting stale `QUEUED` claims; independent of `task_deadline`. |
| `token_budget` | Worker-level per-task token cap; `0` = unlimited. See also `gateway.budget.tokens_per_task`. |
| `agentic_character_budget` | `0` inherits `gateway.truncator.max_input_chars`. |
| `hitl.legacy_handoff_timeout` | Prompt/permission/healing/provider handoffs. Approval, review, and clarification use the 30m approval path. |
| `tool_timeouts` | Per-tool dispatch timeouts; `"default"` applies to unlisted tools. Go duration strings (`"30s"`, `"1m"`). |
| `tool_retries` | Transient failures retried with exponential backoff + jitter; model never sees intermediate failures. Only listed tools are eligible. |

## Plugins

Plugins are directories inside the configured path, each containing a `manifest.json` declaring hooks, capabilities, and env requirements. Load order follows numeric directory prefix (`01-security/`, `02-observability/`, `10-code-quality/`) or the manifest `priority` field. A restart is required to pick up new or changed plugins.
