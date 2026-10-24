package agentic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
)

// breakdownHost answers the breakdown check with err.
type breakdownHost struct {
	*noopHost
	err error
}

func (h *breakdownHost) VerifyBreakdownComplete(context.Context, models.Task) error { return h.err }

// Open subtasks make the finish unverified, which goes to a human. A failed
// read also refuses the finish, but it carries no error code, so the worker
// retries the task instead of handing it to a human.
func TestOpenBreakdownFinishClassifiesOpenSubtasksAndReadErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"open subtasks are unverified", fmt.Errorf("%w: 1 of 2 open", models.ErrBreakdownIncomplete), agenttools.ToolErrorCodeUnverified},
		{"read error is retried", errors.New("breakdown check: list split children: database is locked"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}}
			e := &Engine{host: &breakdownHost{noopHost: &noopHost{}, err: tt.err}}
			stop := e.openBreakdownFinish(context.Background(), task, 1, agentruntime.NewBudgetGuard(nil, task.ID), nil)
			if stop == nil || stop.Status != agentruntime.LoopToolFailure {
				t.Fatalf("stop = %#v, want a refused finish", stop)
			}
			if stop.Meta.ErrorCode != tt.wantCode {
				t.Fatalf("error code = %q, want %q", stop.Meta.ErrorCode, tt.wantCode)
			}
		})
	}
}
