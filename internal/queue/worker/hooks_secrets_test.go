package worker

import (
	"strings"
	"testing"
	"time"

	"agentd/internal/toolenv"
)

// --- CredentialDetectionHook tests ---

func TestCredentialDetectionHook_BlocksOpenAIKey(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"curl 'https://example.com/?key=sk-Abc123456789012345678901234567890123456789012345'"}`,
		CallID:    "call-1",
		SessionID: "sess-1",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for OpenAI key pattern")
	}
	if !strings.Contains(verdict.Reason, "credential pattern") {
		t.Fatalf("reason %q does not mention credential pattern", verdict.Reason)
	}
}

func TestCredentialDetectionHook_BlocksBearerToken(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"token":"eyJhbGciOiJIUzI1NiJ9.eyJ0ZXN0IjoiZGF0YSJ9.abc123"}`,
		CallID:    "call-2",
		SessionID: "sess-2",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for bearer token")
	}
}

func TestCredentialDetectionHook_BlocksGitHubPAT(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"git clone https://ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij@github.com/org/repo"}`,
		CallID:    "call-3",
		SessionID: "sess-3",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for GitHub PAT")
	}
}

func TestCredentialDetectionHook_BlocksAWSAccessKey(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE"}`,
		CallID:    "call-4",
		SessionID: "sess-4",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for AWS access key")
	}
}

func TestCredentialDetectionHook_BlocksGenericAPIKey(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"export API_KEY=abcdef12345678901234"}`,
		CallID:    "call-5",
		SessionID: "sess-5",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for generic API key assignment")
	}
}

func TestCredentialDetectionHook_BlocksGenericAPIKeyJSON(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"api_key":"abcdef12345678901234"}`,
		CallID:    "call-5b",
		SessionID: "sess-5b",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for JSON api_key field")
	}
}

func TestCredentialDetectionHook_BlocksPrivateKey(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "write",
		Args:      `{"path":"id_rsa","content":"-----BEGIN RSA PRIVATE KEY-----\nMIIE..."}`,
		CallID:    "call-6",
		SessionID: "sess-6",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for private key")
	}
}

func TestCredentialDetectionHook_AllowsSafeArgs(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls -la /home/user/project"}`,
		CallID:    "call-7",
		SessionID: "sess-7",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto for safe command: %s", verdict.Reason)
	}
}

func TestCredentialDetectionHook_EmptyArgs(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      "",
		CallID:    "call-8",
		SessionID: "sess-8",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if verdict.Veto {
		t.Fatal("unexpected veto for empty args")
	}
}

func TestCredentialDetectionHook_BlocksSlackToken(t *testing.T) {
	t.Parallel()
	hook := CredentialDetectionHook()
	ctx := HookContext{
		ToolName:  "bash",
		Args:      `{"command":"curl 'https://slack.com/api/chat.postMessage?token=xoxb-1234567890-abcdefghij'"}`,
		CallID:    "call-9",
		SessionID: "sess-9",
		Timestamp: time.Now(),
	}
	verdict, err := hook.Fn(ctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for Slack token")
	}
}

// --- CredentialInjectionHook tests ---

func TestCredentialInjectionHook_InjectsEnv(t *testing.T) {
	const envKey = "TEST_INJECT_CRED_VALUE"
	t.Setenv(envKey, "injected-secret")

	store := NewEnvSecretStore(map[string]string{"github": envKey})
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
	if verdict.Env[0] != wantUser || verdict.Env[1] != wantCanon {
		t.Fatalf("verdict.Env = %v, want [%s, %s]", verdict.Env, wantUser, wantCanon)
	}
}

func TestCredentialInjectionHook_SkipsUnmappedTool(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(map[string]string{"github": "GH_TOKEN"})
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
	store := NewEnvSecretStore(map[string]string{"github": envKey})
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

	store := NewEnvSecretStore(map[string]string{"github": envKey})
	hook := CredentialValidationSessionHook(store)
	err := hook.Fn(HookContext{})
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
}

func TestCredentialValidationSessionHook_FailsWhenMissing(t *testing.T) {
	envKey := "TEST_MISSING_" + strings.ReplaceAll(t.Name(), "/", "_")
	t.Setenv(envKey, "")
	store := NewEnvSecretStore(map[string]string{"github": envKey})
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

	store := NewEnvSecretStore(map[string]string{"github": envKey})
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

	store := NewEnvSecretStore(map[string]string{
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

	executor := NewToolExecutor(nil, t.TempDir(), nil, 0)
	if envContains(executor.envVars, secret) {
		t.Fatal("github secret persisted on executor base envVars")
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

	store := NewEnvSecretStore(map[string]string{"github": envKey})
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
	store := NewEnvSecretStore(map[string]string{"github": envKey})
	hc := NewHookChain()
	hc.RegisterSessionStart(CredentialValidationSessionHook(store))

	err := hc.RunSessionStart(HookContext{})
	if err == nil {
		t.Fatal("RunSessionStart should have failed for missing credential")
	}
}
