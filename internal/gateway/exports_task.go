package gateway

import "agentd/internal/gateway/spec"

// Task context helpers (see spec.WithTaskID).
var (
	WithTaskID        = spec.WithTaskID
	TaskIDFromContext = spec.TaskIDFromContext
)
