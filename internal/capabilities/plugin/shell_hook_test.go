package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/queue/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

func TestShellPreHook_Allow(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "allow.sh", "#!/bin/sh\nexit 0\n")

	entry := HookEntry{
		Name:   "test-allow",
		Script: "allow.sh",
		Policy: "fail_closed",
	}
	hook := ShellPreHook(entry, dir)

	verdict, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		SessionID: "s1",
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.False(t, verdict.Veto)
}

func TestShellPreHook_Veto(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "deny.sh", "#!/bin/sh\necho 'blocked by policy'\nexit 1\n")

	entry := HookEntry{
		Name:   "test-deny",
		Script: "deny.sh",
		Policy: "fail_closed",
	}
	hook := ShellPreHook(entry, dir)

	verdict, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Args:      `{"command":"rm -rf /"}`,
		SessionID: "s1",
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, verdict.Veto)
	assert.Contains(t, verdict.Reason, "blocked by policy")
}

func TestShellPreHook_Timeout(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "slow.sh", "#!/bin/sh\nsleep 10\n")

	entry := HookEntry{
		Name:    "test-slow",
		Script:  "slow.sh",
		Timeout: "100ms",
		Policy:  "fail_closed",
	}
	hook := ShellPreHook(entry, dir)

	verdict, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, verdict.Veto)
	assert.Contains(t, verdict.Reason, "timed out")
}

func TestShellPreHook_EnvVarsPassedToScript(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "env.sh", `#!/bin/sh
if [ "$HOOK_TOOL" = "bash" ] && [ -n "$HOOK_SESSION" ]; then
  exit 0
fi
echo "missing env vars"
exit 1
`)

	entry := HookEntry{
		Name:   "env-check",
		Script: "env.sh",
		Policy: "fail_closed",
	}
	hook := ShellPreHook(entry, dir)

	verdict, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Args:      `{}`,
		SessionID: "session-123",
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.False(t, verdict.Veto)
}

func TestShellPostHook_MutatesResult(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mutate.sh", "#!/bin/sh\necho \"modified: $HOOK_RESULT\"\n")

	entry := HookEntry{
		Name:   "mutate",
		Script: "mutate.sh",
		Policy: "fail_open",
	}
	hook := ShellPostHook(entry, dir)

	result, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Timestamp: time.Now(),
	}, "original")
	require.NoError(t, err)
	assert.Contains(t, result, "modified:")
}

func TestShellPostHook_ErrorReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "fail.sh", "#!/bin/sh\nexit 1\n")

	entry := HookEntry{
		Name:   "fail-post",
		Script: "fail.sh",
		Policy: "fail_closed",
	}
	hook := ShellPostHook(entry, dir)

	_, err := hook.Fn(worker.HookContext{
		ToolName:  "bash",
		Timestamp: time.Now(),
	}, "original")
	require.Error(t, err)
}

func TestParsePolicy(t *testing.T) {
	assert.Equal(t, worker.FailOpen, parsePolicy("fail_open"))
	assert.Equal(t, worker.FailClosed, parsePolicy("fail_closed"))
	assert.Equal(t, worker.FailClosed, parsePolicy(""))
	assert.Equal(t, worker.FailClosed, parsePolicy("unknown"))
}

func TestParseTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Second, parseTimeout("5s"))
	assert.Equal(t, defaultShellTimeout, parseTimeout(""))
	assert.Equal(t, defaultShellTimeout, parseTimeout("invalid"))
	assert.Equal(t, defaultShellTimeout, parseTimeout("0s"))
	assert.Equal(t, defaultShellTimeout, parseTimeout("-1s"))
}
