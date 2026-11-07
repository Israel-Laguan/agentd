// Package runaudit holds the coordinator's per-run accounting: the token
// budget guard every agent call goes through, and the structured audit log.
package runaudit

import (
	"errors"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type BudgetGuard struct {
	tracker spec.BudgetTracker
	taskID  string
}

func NewBudgetGuard(tracker spec.BudgetTracker, taskID string) *BudgetGuard {
	return &BudgetGuard{
		tracker: tracker,
		taskID:  taskID,
	}
}

func (g *BudgetGuard) BeforeCall() error {
	if g.tracker == nil || g.taskID == "" {
		return nil
	}
	return g.tracker.Reserve(g.taskID)
}

func (g *BudgetGuard) AfterCall(tokens int) {
	if g.tracker == nil || g.taskID == "" || tokens <= 0 {
		return
	}
	g.tracker.Add(g.taskID, tokens)
}

func (g *BudgetGuard) Usage() int {
	if g.tracker == nil {
		return 0
	}
	return g.tracker.Usage(g.taskID)
}

func (g *BudgetGuard) IsBudgetExceeded(err error) bool {
	return errors.Is(err, models.ErrBudgetExceeded)
}
