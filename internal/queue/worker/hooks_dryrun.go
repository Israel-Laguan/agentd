package worker

import (
	"encoding/json"
)

// DryRunHook returns a PreHook that intercepts all tool calls and
// returns synthesized results without executing the real handler. The
// verdict uses Veto with a populated Result so the dispatch layer skips
// execution but still runs PostToolUse hooks (audit, scrubbing).
func DryRunHook(enabled bool) PreHook {
	return PreHook{
		Name:   "dry-run",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if !enabled {
				return HookVerdict{}, nil
			}
			return HookVerdict{
				Veto:   true,
				Result: simulatedResult(ctx.ToolName),
			}, nil
		},
	}
}

// simulatedResult returns a plausible synthesized response for each
// built-in tool. Unknown tools receive a generic JSON acknowledgement.
func simulatedResult(tool string) string {
	switch tool {
	case toolNameBash:
		return "(simulated) command executed successfully"
	case toolNameRead:
		return "(simulated) file contents"
	case toolNameWrite:
		return marshalWriteResult()
	default:
		return "(simulated) tool executed successfully"
	}
}

func marshalWriteResult() string {
	out, _ := json.Marshal(map[string]any{
		"success":   true,
		"simulated": true,
	})
	return string(out)
}
