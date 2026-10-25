package intake

import "agentd/internal/gateway/spec"

// The analysis types are still declared in spec. They are aliased here so the
// stage owns its contract surface; moving the declarations is a later step.
type (
	IntentAnalysis = spec.IntentAnalysis
	ScopeAnalysis  = spec.ScopeAnalysis
	ScopeOption    = spec.ScopeOption
)
