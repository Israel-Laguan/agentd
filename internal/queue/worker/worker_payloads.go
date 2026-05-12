package worker

import (
	"fmt"
	"strings"

	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

func failurePayload(result sandbox.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
}

func resultPayload(result sandbox.Result) string {
	return fmt.Sprintf("exit=%d duration=%s\n%s", result.ExitCode, result.Duration, result.Stdout)
}

func promptPayload(command string, detection safety.PromptDetection, result sandbox.Result) string {
	return fmt.Sprintf(
		"pattern=%s command=%q exit=%d duration=%s\n%s",
		detection.Pattern,
		command,
		result.ExitCode,
		result.Duration,
		truncate(strings.TrimSpace(result.Stderr+"\n"+result.Stdout), 1000),
	)
}

func permissionPayload(command string, detection safety.PermissionDetection, result sandbox.Result) string {
	return fmt.Sprintf(
		"pattern=%s command=%q exit=%d duration=%s\n%s",
		detection.Pattern,
		command,
		result.ExitCode,
		result.Duration,
		truncate(strings.TrimSpace(result.Stderr+"\n"+result.Stdout), 1000),
	)
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	suffix := "...[truncated]"
	suffixLen := len([]rune(suffix))
	if max <= suffixLen {
		return string(runes[:max])
	}
	return string(runes[:max-suffixLen]) + suffix
}
