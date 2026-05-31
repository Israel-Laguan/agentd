package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
)

const defaultShellTimeout = 10 * time.Second

var shellHookEnvAllowlist = []string{"PATH"}

// shellHookTestDefault is set from _test.go init; when non-zero it replaces
// defaultShellTimeout for manifests with no explicit timeout field.
var shellHookTestDefault time.Duration

// runScriptCommandHook, when set by tests, replaces subprocess execution.
var runScriptCommandHook func(ctx context.Context, name string, env []string, args ...string) (string, error)

// ShellPreHook wraps a shell script as a PreHook. The script receives
// context via environment variables (HOOK_TOOL, HOOK_ARGS,
// HOOK_SESSION, HOOK_TIMESTAMP). Exit code 0 means allow; non-zero
// means veto with stdout as the reason.
func ShellPreHook(entry HookEntry, pluginDir string) hooks.PreHook {
	timeout := parseTimeout(entry.Timeout)
	policy := parsePolicy(entry.Policy)
	scriptPath := resolveScript(entry.Script, pluginDir)

	return hooks.PreHook{
		Name:   entry.Name,
		Policy: policy,
		Fn: func(ctx hooks.HookContext) (hooks.HookVerdict, error) {
			stdout, err := runScript(scriptPath, timeout, ctx)
			if err != nil {
				reason := strings.TrimSpace(stdout)
				if reason == "" {
					reason = err.Error()
				}
				return hooks.HookVerdict{
					Veto:   true,
					Reason: reason,
				}, nil
			}
			return hooks.HookVerdict{}, nil
		},
	}
}

// ShellPostHook wraps a shell script as a PostHook. The script receives
// the same environment variables plus HOOK_RESULT containing the tool
// result. Stdout replaces the result; a non-zero exit triggers the
// configured failure policy.
func ShellPostHook(entry HookEntry, pluginDir string) hooks.PostHook {
	timeout := parseTimeout(entry.Timeout)
	policy := parsePolicy(entry.Policy)
	scriptPath := resolveScript(entry.Script, pluginDir)

	return hooks.PostHook{
		Name:   entry.Name,
		Policy: policy,
		Fn: func(ctx hooks.HookContext, result string) (string, error) {
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
	script string, timeout time.Duration, ctx hooks.HookContext,
) (string, error) {
	return execScript(script, timeout, hookEnv(ctx))
}

func runScriptWithResult(
	script string, timeout time.Duration,
	ctx hooks.HookContext, result string,
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

	sandboxEnv := agenttools.BuildSandboxEnv(shellHookEnvAllowlist, env)
	stdout, err := runScriptCommand(ctx, script, sandboxEnv)
	if ctx.Err() != nil {
		return stdout, fmt.Errorf("script timed out after %s", timeout)
	}
	if err == nil {
		return stdout, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, err
	}
	// Fall back to /bin/sh for scripts without a shebang or when direct exec is unsupported.
	fallbackOut, fallbackErr := runScriptCommand(ctx, "/bin/sh", sandboxEnv, script)
	if ctx.Err() != nil {
		return fallbackOut, fmt.Errorf("script timed out after %s", timeout)
	}
	if fallbackErr == nil {
		return fallbackOut, nil
	}
	if errors.As(fallbackErr, &exitErr) {
		return fallbackOut, fallbackErr
	}
	return stdout, err
}

func runScriptCommand(ctx context.Context, name string, env []string, args ...string) (string, error) {
	if runScriptCommandHook != nil {
		return runScriptCommandHook(ctx, name, env, args...)
	}
	return runScriptCommandImpl(ctx, name, env, args...)
}

func runScriptCommandImpl(ctx context.Context, name string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // plugin scripts are admin-configured
	cmd.Env = env
	cmd.WaitDelay = 500 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), err
}

func hookEnv(ctx hooks.HookContext) []string {
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
		if shellHookTestDefault > 0 {
			return shellHookTestDefault
		}
		return defaultShellTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultShellTimeout
	}
	return d
}

func parsePolicy(raw string) hooks.FailurePolicy {
	if raw == "fail_open" {
		return hooks.FailOpen
	}
	return hooks.FailClosed
}
