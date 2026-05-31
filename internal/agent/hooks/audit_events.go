package hooks

import (
	"encoding/json"
	"strings"
)

const maxArgumentsSummaryLength = 200
const maxOutputSummaryLength = 1000
const truncationSuffix = "...[truncated]"

type ToolCallEvent struct {
	ToolName         string `json:"tool_name"`
	CallID           string `json:"call_id"`
	ArgumentsSummary string `json:"arguments_summary"`
}

type ToolResultEvent struct {
	ToolName      string `json:"tool_name"`
	CallID        string `json:"call_id"`
	ExitCode      int    `json:"exit_code"`
	DurationMs    int64  `json:"duration_ms"`
	OutputSummary string `json:"output_summary"`
	StdoutBytes   int    `json:"stdout_bytes"`
	StderrBytes   int    `json:"stderr_bytes"`
}

type toolExecEnvelope struct {
	Success    *bool  `json:"Success"`
	ExitCode   *int   `json:"ExitCode"`
	Stdout     string `json:"Stdout"`
	Stderr     string `json:"Stderr"`
	Error      string `json:"error"`
	FatalError string `json:"FatalError"`
}

func truncateToMax(input string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	runes := []rune(input)
	if len(runes) <= maxLength {
		return input
	}
	suffixRunes := []rune(truncationSuffix)
	truncLen := maxLength - len(suffixRunes)
	if truncLen < 0 {
		truncLen = 0
	}
	if truncLen == 0 {
		return string(suffixRunes[:maxLength])
	}
	return string(runes[:truncLen]) + truncationSuffix
}

func parseToolEnv(result string) (*toolExecEnvelope, error) {
	var env toolExecEnvelope
	if err := json.Unmarshal([]byte(result), &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func parseToolExitCode(result string) int {
	env, err := parseToolEnv(result)
	if err != nil {
		trimmed := strings.TrimSpace(result)
		if strings.HasPrefix(trimmed, `{"error"`) || strings.HasPrefix(trimmed, `{"FatalError"`) {
			return -1
		}
		if strings.HasPrefix(trimmed, `{"Success":false`) {
			return -1
		}
		return 0
	}
	if env.Error != "" || env.FatalError != "" {
		return -1
	}
	if env.Success != nil && !*env.Success {
		if env.ExitCode != nil {
			return *env.ExitCode
		}
		return -1
	}
	if env.ExitCode != nil {
		return *env.ExitCode
	}
	return 0
}

func toolResultExitCode(tr ToolResult) int {
	if tr.Status == ToolStatusSuccess {
		return 0
	}
	if tr.ExitCodeSet {
		if tr.ExitCode == 0 && tr.Status != ToolStatusSuccess {
			return -1
		}
		return tr.ExitCode
	}
	switch tr.Status {
	case ToolStatusError, ToolStatusTimeout, ToolStatusVetoed, ToolStatusFatal:
		return -1
	default:
		return parseToolExitCode(tr.Content)
	}
}
