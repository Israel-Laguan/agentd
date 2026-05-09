package queue

import "context"

// Orchestrator is the worker-pool boundary for task execution.
type Orchestrator interface {
	Start(ctx context.Context) error
	Stop() error
}
