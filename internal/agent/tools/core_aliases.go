package tools

import "agentd/internal/agent/hooks"

// Pieces the core uses live in core packages (T-066); these aliases keep the
// harness compiling unchanged until it leaves.

type ToolStatus = hooks.ToolStatus
type ToolError = hooks.ToolError
type ToolResult = hooks.ToolResult

const (
	ToolStatusSuccess = hooks.ToolStatusSuccess
	ToolStatusError   = hooks.ToolStatusError
	ToolStatusTimeout = hooks.ToolStatusTimeout
	ToolStatusVetoed  = hooks.ToolStatusVetoed
	ToolStatusFatal   = hooks.ToolStatusFatal
)

var (
	SuccessResult                 = hooks.SuccessResult
	ErrorResult                   = hooks.ErrorResult
	NonRetryableErrorResult       = hooks.NonRetryableErrorResult
	TimeoutResult                 = hooks.TimeoutResult
	VetoedResult                  = hooks.VetoedResult
	FatalResult                   = hooks.FatalResult
	SchemaRegistryFromDefinitions = hooks.SchemaRegistryFromDefinitions
)
