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
	if g.exceeded && !g.allowFinal {
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

func (g *IterationGuard) reset() {
	if g == nil {
		return
	}
	g.current = 0
	g.exceeded = false
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

// ContextBudgetGuard tracks character budget fill for preemptive summarization
// and typed BudgetExhausted stops.
type ContextBudgetGuard struct {
	totalBudget int
	threshold   float64
	warned      bool
}

// NewContextBudgetGuard creates a guard. threshold 0 disables the warning path.
func NewContextBudgetGuard(totalBudget int, threshold float64) *ContextBudgetGuard {
	return &ContextBudgetGuard{
		totalBudget: totalBudget,
		threshold:   threshold,
	}
}

// Check reports whether the warning threshold was newly crossed and whether the
// hard character budget is exhausted.
func (g *ContextBudgetGuard) Check(chars int) (warn bool, exhausted bool) {
	if g.totalBudget <= 0 {
		return false, false
	}
	exhausted = chars >= g.totalBudget
	if g.threshold <= 0 || g.warned || g.totalBudget <= 0 {
		return false, exhausted
	}
	limit := int(float64(g.totalBudget) * g.threshold)
	if chars >= limit {
		g.warned = true
		return true, exhausted
	}
	return false, exhausted
}

// TotalBudget returns the configured character budget cap.
func (g *ContextBudgetGuard) TotalBudget() int {
	return g.totalBudget
}

func (g *ContextBudgetGuard) reset() {
	if g == nil {
		return
	}
	g.warned = false
}
