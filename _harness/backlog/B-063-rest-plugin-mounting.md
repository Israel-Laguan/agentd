# B-063 (rest): mount plugins safely, with real capability adapters

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog (planned in agentd, never executed; documented in the harness-extraction plan on 2026-11-05  — the builtin loop still runs in agentd, its removal is a pending T-071/T-072 step) |
| Priority | P3 |
| Sprint | — (agentd S15 deferred the full feature) |
| Links | agentd S15 B-063, `capabilities/plugin` (`mounter.go`, `loader.go`, `shell_hook.go`), `queue/worker/worker_plugins.go`, B-061 (rest) |

## Symptoms

The plugin loader, manifest parser, mounter and shell hooks exist, but nothing constructs a `PluginMounter`, so no plugin ever loads. In agentd the `plugins:` config keys were documented as "not implemented" and nothing read them.

## What the feature must decide

- The config key and default for the global plugins directory; whether a project-scoped mount alone enables the mounter; what a missing directory does.
- **Security first:** hook scripts run on the host with `exec.CommandContext`, outside the jail, with only the environment scrubbed. Project-scoped plugins load from `<workspace>/.agentd/plugins/`, which the agent's own `write` tool can fill. Either run hook scripts in the same sandbox as the bash tool, or load plugins only from admin-owned directories and keep project scope off.
- A real `loadCapabilityAdapter`: today it is a stub that always returns `nil, false`, so plugins can add hooks but no capabilities.

## Notes

- Blocks B-061 (rest).
