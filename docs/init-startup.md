# Init and Startup Flow

This note captures the startup path validated during the bootstrap work and the error surfaces that now report useful context even when `--verbose` is not set.

## Findings

- `agentd init` prepares the local home directory, creates or preserves the cron file, opens the SQLite database, applies migrations, and seeds the default agent profiles.
- `agentd start` performs the same runtime preflight, validates provider availability, warms up the active LLM provider, binds the API port, runs boot reconciliation, and then starts the daemon loops.
- The most common startup failures we observed were a stale API key, an unavailable model, and an occupied API port.
- CLI failures now print a short human summary plus technical details so users can fix simple issues without re-running in verbose mode.

## `agentd init`

1. Load configuration from flags, environment, and config files.
2. Ensure the runtime directories exist.
3. Write the default cron file if one is not already present.
4. Open the SQLite database and apply migrations.
5. Seed the built-in agent profiles (`default`, `researcher`, `qa`) with empty `provider` and `model` so tasks cascade through `gateway.order`. Existing profiles are left unchanged on repeat init; use `agentd init --reset-profiles` to force defaults or to clear stale rows that still pin `openai` / `anthropic` from older installs.

Init prints a short hint listing seeded profiles, the first detected LLM provider from `CheckProviders`, and the `--reset-profiles` flag. Enable agentic mode per profile via PATCH (`agentic_mode: true`); empty provider works with agentic when at least one configured backend in order supports chat tools.

If any step fails, the CLI now says what part of init failed and shows the wrapped error chain.

## `agentd start`

1. Load configuration and ensure the runtime directories.
2. Check disk space and writable directories.
3. Verify that at least one LLM provider is available.
4. Open the database.
5. Validate tool credentials.
6. Warm up the selected LLM provider (unless `gateway.warmup_enabled: false` or `agentd start --skip-llm-warmup`).
7. Build the daemon and API server.
8. Bind the API listener.
9. Run boot reconciliation and start the daemon loops.

If startup fails, the CLI now prints a friendly explanation first and then the technical details that caused the failure.

## Error Handling

- Non-verbose runs still show a human-readable failure summary.
- The technical error chain is always printed so the user can see the exact failing operation.
- Common cases such as a busy port, missing write access, or a failed LLM warmup get targeted hints.
