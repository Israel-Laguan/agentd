package worker

import (
	"strings"
	"testing"
	"time"

	"agentd/internal/toolenv"

	wsession "agentd/internal/agent/session"
)

// --- CredentialInjectionHook tests ---

func TestCredentialInjectionHook_InjectsEnv(t *testing.T) {
	const envKey = "TEST_INJECT_CRED_VALUE"
	t.Setenv(envKey, "injected-secret")

	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hook := CredentialInjectionHook(store)
	ctx := HookContext{
		ToolName:  "github",
		Args:      `{"repo":"org/repo"}`,
		CallID:    "call-inject-1",
		SessionID: "sess-inject-1",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
	if len(verdict.Env) != 2 {
		t.Fatalf("verdict.Env = %v, want 2 entries", verdict.Env)
	}
	wantUser := envKey + "=injected-secret"
	wantCanon := toolenv.CredentialEnvKey + "=injected-secret"
	got := map[string]bool{}
	for _, e := range verdict.Env {
		got[e] = true
	}
	if !got[wantUser] || !got[wantCanon] {
		t.Fatalf("verdict.Env = %v, want entries [%s, %s]", verdict.Env, wantUser, wantCanon)
	}
}

func TestCredentialInjectionHook_SkipsUnmappedTool(t *testing.T) {
	t.Parallel()
	store := wsession.NewEnvSecretStore(map[string]string{"github": "GH_TOKEN"})
	hook := CredentialInjectionHook(store)
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"echo hello"}`,
		CallID:    "call-inject-2",
		SessionID: "sess-inject-2",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
	if len(verdict.Env) != 0 {
		t.Fatalf("expected no env injection for unmapped tool, got %v", verdict.Env)
	}
}

func TestCredentialInjectionHook_NilStore(t *testing.T) {
	t.Parallel()
	hook := CredentialInjectionHook(nil)
	ctx := HookContext{
		ToolName:  "github",
		CallID:    "call-inject-3",
		SessionID: "sess-inject-3",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("unexpected veto with nil store")
	}
}

func TestCredentialInjectionHook_NoEnvWhenUnset(t *testing.T) {
	const envKey = "TEST_INJECT_UNSET_CRED"
	t.Setenv(envKey, "")
	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hook := CredentialInjectionHook(store)
	ctx := HookContext{
		ToolName:  "github",
		CallID:    "call-inject-4",
		SessionID: "sess-inject-4",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("unexpected veto when credential env unset")
	}
	if len(verdict.Env) != 0 {
		t.Fatalf("expected no env injection when unset, got %v", verdict.Env)
	}
}

// --- CredentialValidationSessionHook tests ---

func TestCredentialValidationSessionHook_PassesWhenSet(t *testing.T) {
	const envKey = "TEST_CRED_VALIDATION_OK"
	t.Setenv(envKey, "present-value")

	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hook := CredentialValidationSessionHook(store)
	err := hook.Fn(HookContext{})
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
}

func TestCredentialValidationSessionHook_FailsWhenMissing(t *testing.T) {
	envKey := "TEST_MISSING_" + strings.ReplaceAll(t.Name(), "/", "_")
	t.Setenv(envKey, "")
	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hook := CredentialValidationSessionHook(store)
	err := hook.Fn(HookContext{})
	if err == nil {
		t.Fatal("expected error for missing credential, got nil")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("error %q does not mention tool name", err.Error())
	}
}

func TestCredentialValidationSessionHook_NilStore(t *testing.T) {
	t.Parallel()
	hook := CredentialValidationSessionHook(nil)
	err := hook.Fn(HookContext{})
	if err != nil {
		t.Fatalf("hook returned error with nil store: %v", err)
	}
}

// --- Integration: detection + injection don't leak credentials ---

func TestCredentials_NeverAppearInAuditPayload(t *testing.T) {
	const envKey = "TEST_AUDIT_LEAK_CHECK"
	t.Setenv(envKey, "super-secret-token-abc123")

	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hc := NewHookChain()
	hc.RegisterPre(CredentialInjectionHook(store))
	hc.RegisterPre(CredentialDetectionHook())

	ctx := HookContext{
		ToolName:  "github",
		Args:      `{"repo":"org/repo"}`,
		CallID:    "call-audit-1",
		SessionID: "sess-audit-1",
		Timestamp: time.Now(),
	}

	verdict := hc.RunPre(ctx)
	if verdict.Veto {
		t.Fatalf("hook chain vetoed clean args: %s", verdict.Reason)
	}
	if strings.Contains(ctx.Args, "super-secret-token-abc123") {
		t.Fatal("credential leaked into tool arguments")
	}
	envFound := false
	for _, pair := range verdict.Env {
		if strings.Contains(pair, "super-secret-token-abc123") {
			envFound = true
			break
		}
	}
	if !envFound {
		t.Fatalf("credential not found in per-call env, got %v", verdict.Env)
	}
}

func TestCredentials_DoNotLeakAcrossToolCalls(t *testing.T) {
	const githubKey = "TEST_ISOLATION_GITHUB_CRED"
	const gitlabKey = "TEST_ISOLATION_GITLAB_CRED"
	const secret = "super-secret-github-only"
	t.Setenv(githubKey, secret)
	t.Setenv(gitlabKey, "")

	store := wsession.NewEnvSecretStore(map[string]string{
		"github": githubKey,
		"gitlab": gitlabKey,
	})
	hc := NewHookChain()
	hc.RegisterPre(CredentialInjectionHook(store))
	hc.RegisterPre(CredentialDetectionHook())

	githubVerdict := hc.RunPre(HookContext{
		ToolName:  "github",
		Args:      `{"repo":"org/repo"}`,
		CallID:    "call-iso-1",
		SessionID: "sess-iso-1",
		Timestamp: time.Now(),
	})
	if githubVerdict.Veto {
		t.Fatalf("github hook vetoed: %s", githubVerdict.Reason)
	}
	if !envContains(githubVerdict.Env, secret) {
		t.Fatalf("github env %v should contain secret", githubVerdict.Env)
	}

	gitlabVerdict := hc.RunPre(HookContext{
		ToolName:  "gitlab",
		Args:      `{"project":"org/proj"}`,
		CallID:    "call-iso-2",
		SessionID: "sess-iso-2",
		Timestamp: time.Now(),
	})
	if gitlabVerdict.Veto {
		t.Fatalf("gitlab hook vetoed: %s", gitlabVerdict.Reason)
	}
	if envContains(gitlabVerdict.Env, secret) {
		t.Fatalf("gitlab env %v must not contain github secret", gitlabVerdict.Env)
	}

	executor := NewToolExecutor(nil, t.TempDir(), []string{"BASE=1"}, 0)
	githubEnv := executor.BuildEnv(githubVerdict.Env...)
	if !envContains(githubEnv, secret) {
		t.Fatalf("github BuildEnv %v should contain secret", githubEnv)
	}
	gitlabEnv := executor.BuildEnv(gitlabVerdict.Env...)
	if envContains(gitlabEnv, secret) {
		t.Fatalf("gitlab BuildEnv %v must not contain github secret", gitlabEnv)
	}
	if envContains(executor.envVars, secret) {
		t.Fatalf("github secret leaked into executor base envVars: %v", executor.envVars)
	}
}

func envContains(env []string, secret string) bool {
	for _, pair := range env {
		if strings.Contains(pair, secret) {
			return true
		}
	}
	return false
}

// --- HookChain integration ---

func TestHookChain_CredentialDetection_VetoesInChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(CredentialDetectionHook())

	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"curl 'https://example.com/?key=sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'"}`,
		CallID:    "call-chain-1",
		SessionID: "sess-chain-1",
		Timestamp: time.Now(),
	}
	verdict := hc.RunPre(ctx)
	if !verdict.Veto {
		t.Fatal("chain RunPre did not veto credential in args")
	}
}

func TestHookChain_CredentialDetection_PassesSafeArgs(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(CredentialDetectionHook())

	ctx := HookContext{
		ToolName:  "read",
		Args:      `{"path":"README.md"}`,
		CallID:    "call-chain-2",
		SessionID: "sess-chain-2",
		Timestamp: time.Now(),
	}
	verdict := hc.RunPre(ctx)
	if verdict.Veto {
		t.Fatalf("chain RunPre falsely vetoed: %s", verdict.Reason)
	}
}

func TestHookChain_SessionStart_CredentialValidation(t *testing.T) {
	const envKey = "TEST_SESSION_HOOK_CRED"
	t.Setenv(envKey, "ok")

	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hc := NewHookChain()
	hc.RegisterSessionStart(CredentialValidationSessionHook(store))

	err := hc.RunSessionStart(HookContext{})
	if err != nil {
		t.Fatalf("RunSessionStart failed: %v", err)
	}
}

func TestHookChain_SessionStart_FailsOnMissingCredential(t *testing.T) {
	envKey := "TEST_MISSING_" + strings.ReplaceAll(t.Name(), "/", "_")
	t.Setenv(envKey, "")
	store := wsession.NewEnvSecretStore(map[string]string{"github": envKey})
	hc := NewHookChain()
	hc.RegisterSessionStart(CredentialValidationSessionHook(store))

	err := hc.RunSessionStart(HookContext{})
	if err == nil {
		t.Fatal("RunSessionStart should have failed for missing credential")
	}
}
