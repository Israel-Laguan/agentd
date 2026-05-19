package worker

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// credentialPatterns matches common secret formats that should never
// appear in tool arguments. Each pattern is compiled once at init and
// tested against the raw arguments string.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9]{20,}`),                      // OpenAI-style API key
	regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*\S{8,}`),   // generic api_key=...
	regexp.MustCompile(`(?i)(?:secret|token|password|passwd|credential)\s*[:=]\s*\S{8,}`), // generic secret/token/password assignments
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/]+=*`),          // Bearer token
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),                         // GitHub PAT
	regexp.MustCompile(`gho_[A-Za-z0-9]{36,}`),                         // GitHub OAuth
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                 // GitHub fine-grained PAT
	regexp.MustCompile(`xox[bpras]-[A-Za-z0-9\-]{10,}`),               // Slack token
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                             // AWS access key ID
	regexp.MustCompile(`-----BEGIN\s+(RSA\s+)?PRIVATE KEY-----`),       // PEM private key
}

// CredentialDetectionHook returns a PreHook that vetoes tool calls
// whose arguments contain patterns matching common secret formats
// (API keys, tokens, passwords, etc.). This prevents the model from
// accidentally embedding credentials in tool arguments where they
// would enter the context window and audit logs.
func CredentialDetectionHook() PreHook {
	return PreHook{
		Name:   "credential-detection",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if ctx.Args == "" {
				return HookVerdict{}, nil
			}
			for _, pat := range credentialPatterns {
				if pat.MatchString(ctx.Args) {
					slog.Warn("credential detected in tool arguments",
						"hook", "credential-detection",
						"tool", ctx.ToolName,
						"call_id", ctx.CallID,
						"session_id", ctx.SessionID,
						"pattern", pat.String(),
					)
					return HookVerdict{
						Veto: true,
						Reason: fmt.Sprintf(
							"Tool call %q blocked: arguments contain a value matching a credential pattern. "+
								"Credentials must be injected via the execution environment, not passed as arguments.",
							ctx.ToolName,
						),
					}, nil
				}
			}
			return HookVerdict{}, nil
		},
	}
}

// CredentialInjectionHook returns a PreHook that populates the tool's
// execution environment with credentials from the SecretStore. The
// credentials are injected as environment variable pairs (KEY=VALUE)
// appended to the ToolExecutor's envVars so that tool handlers can
// read them from their environment without the values ever appearing
// in tool arguments.
//
// The hook itself never vetoes; it is FailOpen because injection
// failure should not block execution (the tool handler will fail with
// a clear "missing credential" error from its own env lookup).
func CredentialInjectionHook(store SecretStore, executor *ToolExecutor) PreHook {
	return PreHook{
		Name:   "credential-injection",
		Policy: FailOpen,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if store == nil || executor == nil {
				return HookVerdict{}, nil
			}
			val, ok := store.Get(ctx.ToolName)
			if !ok {
				return HookVerdict{}, nil
			}
			envVar := envVarForTool(store, ctx.ToolName)
			if envVar == "" {
				return HookVerdict{}, nil
			}
			executor.InjectEnv(envVar + "=" + val)
			return HookVerdict{}, nil
		},
	}
}

// envVarForTool extracts the env var name from an EnvSecretStore for
// a given tool. Falls back to empty string for non-EnvSecretStore
// implementations.
func envVarForTool(store SecretStore, tool string) string {
	if es, ok := store.(*EnvSecretStore); ok {
		es.mu.RLock()
		defer es.mu.RUnlock()
		return es.mappings[tool]
	}
	return strings.ToUpper(strings.ReplaceAll(tool, "-", "_")) + "_CREDENTIAL"
}

// CredentialValidationSessionHook returns a SessionStartHook that
// validates all tool credentials are present at startup. Tools that
// require credentials (as declared in config) fail early if the
// backing environment variable is unset.
func CredentialValidationSessionHook(store SecretStore) SessionStartHook {
	return SessionStartHook{
		Name:   "credential-validation",
		Policy: FailClosed,
		Fn: func(_ HookContext) error {
			if store == nil {
				return nil
			}
			es, ok := store.(*EnvSecretStore)
			if !ok {
				return nil
			}
			return es.Validate()
		},
	}
}
