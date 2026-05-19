package toolenv

import (
	"context"
	"strings"
)

// CredentialEnvKey is the well-known env var name adapters use to read the
// injected credential value. CredentialInjectionHook sets this in addition to
// any user-configured env var from tool_credentials config.
const CredentialEnvKey = "AGENTD_TOOL_CREDENTIAL"

type ctxKey struct{}

// With attaches per-call KEY=VALUE environment pairs to ctx for capability tools.
func With(ctx context.Context, env []string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	cp := append([]string(nil), env...)
	return context.WithValue(ctx, ctxKey{}, cp)
}

// From returns per-call env pairs previously attached with With.
func From(ctx context.Context) []string {
	v, _ := ctx.Value(ctxKey{}).([]string)
	if len(v) == 0 {
		return nil
	}
	return append([]string(nil), v...)
}

// CredentialValue returns the value of CredentialEnvKey from env, scanning
// from the end so the most recent override wins. Returns "" if unset.
func CredentialValue(env []string) string {
	prefix := CredentialEnvKey + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}
