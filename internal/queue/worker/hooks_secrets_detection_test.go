package worker

import (
	agenthooks "agentd/internal/agent/hooks"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- CredentialDetectionHook tests ---

func TestCredentialDetectionHook_BlocksOpenAIKey(t *testing.T) {
	t.Parallel()
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	cases := []struct {
		name  string
		token string
	}{
		{"long", strings.Repeat("A", 24)},
		{"short_hyphenated", "secret-token"},
		{"short_alphanumeric", "abc12345"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bearerValue := "Bearer " + tc.token
			ctx := agenthooks.HookContext{
				ToolName:  "bash",
				Args:      fmt.Sprintf(`{"authorization":"%s"}`, bearerValue),
				CallID:    "call-2-" + tc.name,
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
		})
	}
}

func TestCredentialDetectionHook_AllowsShortBearerLikePhrases(t *testing.T) {
	t.Parallel()
	hook := agenthooks.CredentialDetectionHook()
	cases := []struct {
		name string
		args string
	}{
		{"authentication_phrase", `{"command":"echo 'This API uses bearer authentication'"}`},
		{"dotted_abbreviation", `{"note":"docs mention Bearer x.y scheme"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict, err := hook.Fn(agenthooks.HookContext{
				ToolName:  "bash",
				Args:      tc.args,
				CallID:    "call-2c-" + tc.name,
				SessionID: "sess-2c",
				Timestamp: time.Now(),
			})
			if err != nil {
				t.Fatalf("hook returned error: %v", err)
			}
			if verdict.Veto {
				t.Fatalf("unexpected veto for %s: %s", tc.name, verdict.Reason)
			}
		})
	}
}

func TestCredentialDetectionHook_BlocksGitHubPAT(t *testing.T) {
	t.Parallel()
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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

func TestCredentialDetectionHook_AllowsCompoundTokenFields(t *testing.T) {
	t.Parallel()
	hook := agenthooks.CredentialDetectionHook()
	cases := []struct {
		name string
		args string
	}{
		{"access_token", `{"access_token":"next_page_cursor123"}`},
		{"page_token", `{"page_token":"pagination_cursor_xyz"}`},
		{"refresh_token", `{"refresh_token":"opaque_cursor_value"}`},
		{"client_secret", `{"client_secret":"config_placeholder_not_real"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict, err := hook.Fn(agenthooks.HookContext{
				ToolName:  "mcp_tool",
				Args:      tc.args,
				CallID:    "call-compound-" + tc.name,
				SessionID: "sess-compound",
				Timestamp: time.Now(),
			})
			if err != nil {
				t.Fatalf("hook returned error: %v", err)
			}
			if verdict.Veto {
				t.Fatalf("unexpected veto for %s: %s", tc.name, verdict.Reason)
			}
		})
	}
}

func TestCredentialDetectionHook_AllowsSafeArgs(t *testing.T) {
	t.Parallel()
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	ctx := agenthooks.HookContext{
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
	hook := agenthooks.CredentialDetectionHook()
	slackToken := "xoxb-" + strings.Repeat("1", 10) + "-" + strings.Repeat("a", 10)
	ctx := agenthooks.HookContext{
		ToolName:  "bash",
		Args:      fmt.Sprintf(`{"command":"curl 'https://slack.com/api/chat.postMessage?token=%s'"}`, slackToken),
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
