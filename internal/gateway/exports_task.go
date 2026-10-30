package gateway

import "agentd/internal/gateway/spec"

// Task context helpers (see spec.WithTaskID).
var (
	WithTaskID        = spec.WithTaskID
	TaskIDFromContext = spec.TaskIDFromContext
	// AttrsForTask builds a task-backed call's routing attributes.
	AttrsForTask = spec.AttrsForTask
)
