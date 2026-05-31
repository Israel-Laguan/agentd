package worker

import agenttools "agentd/internal/agent/tools"

type ToolExecutor = agenttools.ToolExecutor
type ToolResult = agenttools.ToolResult
type ToolStatus = agenttools.ToolStatus
type ToolError = agenttools.ToolError
type RetryConfig = agenttools.RetryConfig
type RetryDispatchFunc = agenttools.RetryDispatchFunc
type RetryingExecutor = agenttools.RetryingExecutor
type ToolManifest = agenttools.ToolManifest
type TaskClassification = agenttools.TaskClassification
type TaskClassifier = agenttools.TaskClassifier
type toolFailureTracker = agenttools.ToolFailureTracker

const (
	toolNameBash             = agenttools.ToolNameBash
	toolNameRead             = agenttools.ToolNameRead
	toolNameWrite            = agenttools.ToolNameWrite
	toolNameDelegate         = agenttools.ToolNameDelegate
	toolNameDelegateParallel = agenttools.ToolNameDelegateParallel
	toolErrorPrefix          = agenttools.ToolErrorPrefix

	ToolStatusSuccess = agenttools.ToolStatusSuccess
	ToolStatusError   = agenttools.ToolStatusError
	ToolStatusTimeout = agenttools.ToolStatusTimeout
	ToolStatusVetoed  = agenttools.ToolStatusVetoed
	ToolStatusFatal   = agenttools.ToolStatusFatal

	TaskTypeSummarize   = agenttools.TaskTypeSummarize
	TaskTypeCodeGen     = agenttools.TaskTypeCodeGen
	TaskTypeDocQA       = agenttools.TaskTypeDocQA
	TaskTypeWebResearch = agenttools.TaskTypeWebResearch
	TaskTypeFullAgent   = agenttools.TaskTypeFullAgent
)

var (
	NewToolExecutor     = agenttools.NewToolExecutor
	NewRetryingExecutor = agenttools.NewRetryingExecutor
	NewToolManifest     = agenttools.NewToolManifest
	NewTaskClassifier   = agenttools.NewTaskClassifier
	BuildSandboxEnv     = agenttools.BuildSandboxEnv

	SuccessResult           = agenttools.SuccessResult
	ErrorResult             = agenttools.ErrorResult
	NonRetryableErrorResult = agenttools.NonRetryableErrorResult
	TimeoutResult           = agenttools.TimeoutResult
	VetoedResult            = agenttools.VetoedResult
	FatalResult             = agenttools.FatalResult

	SchemaRegistryFromDefinitions = agenttools.SchemaRegistryFromDefinitions
	filterToolsByNames            = agenttools.FilterToolsByNames

	jsonErrorf           = agenttools.JSONErrorf
	sandboxFailureJSON   = agenttools.SandboxFailureJSON
	isToolErrorPayload   = agenttools.IsToolErrorPayload
	stripToolErrorPrefix = agenttools.StripToolErrorPrefix
)

func newToolFailureTracker(threshold int) *toolFailureTracker {
	return agenttools.NewToolFailureTracker(threshold)
}
