package hooks

import "agentd/internal/gateway"

// Tool names the hooks match on; the builtin's executor serves the same names.
const (
	ToolNameBash             = "bash"
	ToolNameRead             = "read"
	ToolNameWrite            = "write"
	ToolNameDelegate         = "delegate"
	ToolNameDelegateParallel = "delegate_parallel"
)

// SchemaRegistryFromDefinitions builds a tool-name → parameters map for SchemaValidationHook.
func SchemaRegistryFromDefinitions(defs []gateway.ToolDefinition) map[string]*gateway.FunctionParameters {
	registry := make(map[string]*gateway.FunctionParameters, len(defs))
	for _, def := range defs {
		if def.Parameters != nil {
			registry[def.Name] = def.Parameters
		}
	}
	return registry
}
