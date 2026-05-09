package sandbox

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

type stubExec struct {
	res models.ExecutionResult
	err error
}

func (s stubExec) Execute(context.Context, models.ExecutionPayload) (models.ExecutionResult, error) {
	return s.res, s.err
}

func TestEnvironmentAdapterNilExecutor(t *testing.T) {
	a := EnvironmentAdapter{}
	out := a.Execute(context.Background(), models.ExecutionPayload{Command: "x"})
	if out.Success || out.FatalError == "" {
		t.Fatalf("out = %#v", out)
	}
}

func TestEnvironmentAdapterMapsErrorAndOutput(t *testing.T) {
	a := EnvironmentAdapter{Executor: stubExec{
		res: models.ExecutionResult{Stdout: "o", Stderr: "e", Success: false},
		err: errors.New("boom"),
	}}
	out := a.Execute(context.Background(), models.ExecutionPayload{})
	if out.FatalError != "boom" {
		t.Fatalf("fatal = %q", out.FatalError)
	}
	if out.Output == "" {
		t.Fatal("expected output fallback")
	}
}
