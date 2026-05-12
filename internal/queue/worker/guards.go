package worker

import (
	"context"
	"errors"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

const iterationExceededMessage = "You have reached the maximum number of tool call iterations. Please provide your final response now."

type IterationGuard struct {
	maxIterations int
	current       int
	exceeded      bool
	allowFinal    bool
}

func NewIterationGuard(maxIterations int) *IterationGuard {
	return &IterationGuard{
		maxIterations: maxIterations,
		current:       0,
		exceeded:      false,
		allowFinal:    false,
	}
}

func (g *IterationGuard) BeforeIteration() error {
	if g.exceeded {
		return errors.New("iteration limit exceeded")
	}
	return nil
}

func (g *IterationGuard) AfterIteration(hasToolCalls bool) {
	if !hasToolCalls {
		return
	}
	g.current++
	if g.current >= g.maxIterations && !g.exceeded {
		g.exceeded = true
		g.allowFinal = true
	}
}

func (g *IterationGuard) IsExceeded() bool {
	return g.exceeded
}

func (g *IterationGuard) ShouldInjectFinalMessage() bool {
	return g.exceeded && g.allowFinal
}

func (g *IterationGuard) FinalMessage() gateway.PromptMessage {
	return gateway.PromptMessage{
		Role:    "user",
		Content: iterationExceededMessage,
	}
}

func (g *IterationGuard) ResetAllowFinal() {
	g.allowFinal = false
}

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

type DeadlineGuard struct {
	deadline time.Time
}

func NewDeadlineGuard(ctx context.Context) *DeadlineGuard {
	deadline := time.Now().Add(24 * time.Hour)
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}
	return &DeadlineGuard{
		deadline: deadline,
	}
}

func (g *DeadlineGuard) BeforeIteration() error {
	if time.Now().After(g.deadline) {
		return errors.New("task deadline already expired")
	}
	return nil
}

func (g *DeadlineGuard) Remaining() time.Duration {
	return time.Until(g.deadline)
}

func (g *DeadlineGuard) Deadline() time.Time {
	return g.deadline
}
