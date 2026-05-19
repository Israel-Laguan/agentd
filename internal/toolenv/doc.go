// Package toolenv propagates per-call KEY=VALUE environment pairs through
// context for capability tools (e.g. MCP adapters). It also defines the
// canonical credential env var name and lookup used when adapters need a
// single credential value for authentication.
//
// toolenv does not resolve credentials from SecretStore, run hook chains,
// scan tool arguments for secrets, or wire HTTP transports; see
// internal/queue/worker for hooks and internal/capabilities/mcp for MCP auth.
package toolenv
