# LiteLLM Integration Notes

Findings from running agentd behind a real LiteLLM proxy (1.100.0) on rootless
Podman. For configuring LiteLLM as a provider, see
[openai-compatible-providers.md](openai-compatible-providers.md#litellm-managed-proxy).

## Task correlation: `send_task_metadata`

With `options.send_task_metadata: true` on an `openai`-adapter provider, every
request agentd sends carries a `metadata` map:

```json
"metadata": { "task_id": "<task uuid>", "agent_id": "default", "role": "worker" }
```

Each key is set only when non-empty. The top-level `user` field is never set.
Worker requests always carry all three keys.

**LiteLLM keeps this metadata and does not forward it.** LiteLLM treats
`metadata` as its own field for spend logs and tags, and that is what the
option is for: per-task spend correlation *inside LiteLLM*. It does not reach
the model backend behind the proxy. Checked against LiteLLM 1.100.0 with a
request-echo upstream:

| Sent by the client | Reaches the upstream? |
| --- | --- |
| `metadata` | No. LiteLLM keeps it for spend logging. |
| `user` | No |
| Client `x-*` headers | No (not by default) |
| Unknown top-level keys (e.g. `agentd_metadata`) | Yes, unchanged |

On the agentd side, the in-process test
[`plan_execute_integration_test.go`](../internal/queue/worker/plan_execute_integration_test.go)
checks that `metadata` goes out on the wire with the right values on every
worker request. It uses the real `openai` adapter over HTTP against a fake
OpenAI server.

### Forwarding the correlation upstream

If the backend behind LiteLLM needs the task id (for example a mock that tags
evidence per task, or a backend with its own logging), use a LiteLLM
`CustomLogger` pre-call hook to copy the fields into a top-level key. LiteLLM
forwards that key. Don't change agentd to send extra top-level keys itself:
strict OpenAI-compatible endpoints reject unknown request parameters.

`agentd_correlation.py`, placed next to the LiteLLM config file:

```python
from litellm.integrations.custom_logger import CustomLogger

FORWARD_KEY = "agentd_metadata"
FIELDS = ("task_id", "agent_id", "role")


class AgentdCorrelation(CustomLogger):
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        metadata = data.get("metadata") or data.get("litellm_metadata") or {}
        fields = {key: metadata[key] for key in FIELDS if metadata.get(key)}
        if fields:
            data[FORWARD_KEY] = fields
        return data


proxy_handler_instance = AgentdCorrelation()
```

Register it in the LiteLLM config. The module path resolves relative to the
config file's directory:

```yaml
litellm_settings:
  callbacks: agentd_correlation.proxy_handler_instance
```

In a container, mount the hook beside the config
(`/app/config.yaml` and `/app/agentd_correlation.py`). If the config lists the
callback but the file is missing, LiteLLM exits on startup with
`ModuleNotFoundError: No module named 'agentd_correlation'`. The upstream then
receives `"agentd_metadata": {"task_id": …, "agent_id": …, "role": …}`.

The dev stack uses exactly this: [`dev/litellm/`](../dev/litellm/), wired in
[`docker-compose.dev.yml`](../docker-compose.dev.yml). Its mock backend,
[`dev/mockllm/server.py`](../dev/mockllm/server.py), reads `agentd_metadata`
first, then `metadata`.

## Listing tasks: pagination

`GET /api/v1/projects/{id}/tasks` is paginated: default `limit` 25, maximum
200, sorted by `created_at` descending (newest first). A client or test that
omits `limit` silently loses the oldest tasks once a project has more than 25.
Those are usually the original plan tasks. Pass `?limit=200` (and `offset` for
more), and compare `meta.total` with the rows returned. See
[api-tasks.md](api-tasks.md).

## Container deploy notes (Podman)

Podman + `podman-compose` 1.3.0 is the target runtime. These are the traps we
hit running agentd, LiteLLM and a mock backend under compose:

- **`entrypoint` vs `command:`.** The agentd image sets
  `ENTRYPOINT ["agentd"]`. A compose `command:` only replaces CMD, so
  `command: sh -c "…"` runs `agentd sh -c "…"`. Override `entrypoint:` when
  the service needs a shell.
- **Make `command:` a single-element list.** With `entrypoint: ["sh", "-c"]`,
  a string-form `command:` is shlex-split into argv. `sh -c` then takes only
  the first token (`agentd`) as its script and silently drops the rest, so the
  container prints help and exits 0. Pass the whole script as one list item:

  ```yaml
  entrypoint: ["sh", "-c"]
  command:
    - >-
      agentd --config /etc/agentd/config.yaml init &&
      exec agentd --config /etc/agentd/config.yaml start --skip-llm-warmup -v
  ```

  Or put the whole `sh -c` line in `entrypoint:`, as `docker-compose.dev.yml` does.
- **Use `CMD-SHELL` healthchecks.** podman-compose mangles the quoting of
  array-form `["CMD", "python", "-c", "…"]` healthchecks, and the service stays
  `unhealthy` forever. Use `["CMD-SHELL", "python -c \"…\""]`.
- **Volume ownership: `userns_mode: keep-id` + `x-podman: in_pod: false`.**
  Under rootless Podman, a fresh named volume shared by a root container and
  agentd (uid 1000) ends up owned by the root container's mapping, and agentd
  gets `permission denied` creating project workspaces. `:U` on the mount
  does not fix a fresh volume. Map agentd's uid to the host user with
  `userns_mode: "keep-id:uid=1000,gid=1000"`. That needs the top-level
  `x-podman: { in_pod: false }`, because podman-compose's default shared pod
  rejects per-container `--userns`. Service DNS still works over the network.
- **`down -v` does not remove named volumes under podman-compose.** For a
  clean state, remove them explicitly:
  `podman compose down; podman volume rm -f <project>_<volume>`.
