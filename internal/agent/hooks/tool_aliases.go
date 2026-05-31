package hooks

import agenttools "agentd/internal/agent/tools"

const (
	toolNameBash             = agenttools.ToolNameBash
	toolNameRead             = agenttools.ToolNameRead
	toolNameWrite            = agenttools.ToolNameWrite
	toolNameDelegate         = agenttools.ToolNameDelegate
	toolNameDelegateParallel = agenttools.ToolNameDelegateParallel
	toolErrorPrefix          = agenttools.ToolErrorPrefix
)

type ToolResult = agenttools.ToolResult
type ToolStatus = agenttools.ToolStatus

const (
	ToolStatusSuccess = agenttools.ToolStatusSuccess
	ToolStatusError   = agenttools.ToolStatusError
	ToolStatusTimeout = agenttools.ToolStatusTimeout
	ToolStatusVetoed  = agenttools.ToolStatusVetoed
	ToolStatusFatal   = agenttools.ToolStatusFatal
)

var (
	isToolErrorPayload            = agenttools.IsToolErrorPayload
	stripToolErrorPrefix          = agenttools.StripToolErrorPrefix
	SchemaRegistryFromDefinitions = agenttools.SchemaRegistryFromDefinitions
)
