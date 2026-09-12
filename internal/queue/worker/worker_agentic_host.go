package worker

import "agentd/internal/queue/worker/agentic"

var _ agentic.Host = (*Worker)(nil)
