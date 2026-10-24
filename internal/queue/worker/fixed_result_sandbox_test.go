package worker

import (
	"context"

	"agentd/internal/sandbox"
)

type fixedResultSandbox struct {
	result sandbox.Result
	err    error
}

func (s fixedResultSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	return s.result, s.err
}
