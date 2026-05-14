package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/queue/worker"
)

const defaultShellTimeout = 3 * time.Second

// ShellPreHook wraps a shell script as a PreHook. The script receives
// context via environment variables (HOOK_TOOL, HOOK_ARGS,
// HOOK_SESSION, HOOK_TIMESTAMP). Exit code 0 means allow; non-zero
// means veto with stdout as the reason.
func ShellPreHook(entry HookEntry, pluginDir string) worker.PreHook {
	timeout := parseTimeout(entry.Timeout)
	policy := parsePolicy(entry.Policy)
	scriptPath := resolveScript(entry.Script, pluginDir)

	return worker.PreHook{
		Name:   entry.Name,
		Policy: policy,
		Fn: func(ctx worker.HookContext) (worker.HookVerdict, error) {
			stdout, err := runScript(scriptPath, timeout, ctx)
			if err != nil {
				reason := strings.TrimSpace(stdout)
				if reason == "" {
					reason = err.Error()
				}
				return worker.HookVerdict{
					Veto:   true,
					Reason: reason,
				}, nil
			}
			return worker.HookVerdict{}, nil
		},
	}
}

// ShellPostHook wraps a shell script as a PostHook. The script receives
// the same environment variables plus HOOK_RESULT containing the tool
// result. Stdout replaces the result; a non-zero exit triggers the
// configured failure policy.
func ShellPostHook(entry HookEntry, pluginDir string) worker.PostHook {
	timeout := parseTimeout(entry.Timeout)
	policy := parsePolicy(entry.Policy)
	scriptPath := resolveScript(entry.Script, pluginDir)

	return worker.PostHook{
		Name:   entry.Name,
		Policy: policy,
		Fn: func(ctx worker.HookContext, result string) (string, error) {
			stdout, err := runScriptWithResult(
				scriptPath, timeout, ctx, result,
			)
			if err != nil {
				return "", fmt.Errorf("shell hook %s: %w", entry.Name, err)
			}
			out := strings.TrimSpace(stdout)
			if out != "" {
				return out, nil
			}
			return result, nil
		},
	}
}

func runScript(
	script string, timeout time.Duration, ctx worker.HookContext,
) (string, error) {
	return execScript(script, timeout, hookEnv(ctx))
}

func runScriptWithResult(
	script string, timeout time.Duration,
	ctx worker.HookContext, result string,
) (string, error) {
	env := hookEnv(ctx)
	env = append(env, "HOOK_RESULT="+result)
	return execScript(script, timeout, env)
}

func execScript(
	script string, timeout time.Duration, env []string,
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", script) //nolint:gosec // plugin scripts are admin-configured
	cmd.Env = env
	cmd.WaitDelay = 500 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("script timed out after %s", timeout)
	}
	if err != nil {
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func hookEnv(ctx worker.HookContext) []string {
	return []string{
		"HOOK_TOOL=" + ctx.ToolName,
		"HOOK_ARGS=" + ctx.Args,
		"HOOK_SESSION=" + ctx.SessionID,
		"HOOK_TIMESTAMP=" + ctx.Timestamp.Format(time.RFC3339),
	}
}

func resolveScript(script, pluginDir string) string {
	if filepath.IsAbs(script) {
		return script
	}
	return filepath.Join(pluginDir, script)
}

func parseTimeout(raw string) time.Duration {
	if raw == "" {
		return defaultShellTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultShellTimeout
	}
	return d
}

func parsePolicy(raw string) worker.FailurePolicy {
	if raw == "fail_open" {
		return worker.FailOpen
	}
	return worker.FailClosed
}
