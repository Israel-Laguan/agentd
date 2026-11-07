package session

import "agentd/internal/agent/hooks"

// The secret store lives in agent/hooks, where the core uses it (T-066); these
// aliases keep the harness compiling unchanged until it leaves.

type SecretStore = hooks.SecretStore
type ToolCredentialMapping = hooks.ToolCredentialMapping
type EnvSecretStore = hooks.EnvSecretStore

var NewEnvSecretStore = hooks.NewEnvSecretStore
