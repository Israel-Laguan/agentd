package gateway

import (
	"fmt"
	"sync"

	"agentd/internal/models"
)

// InMemoryBudgetTracker is a concurrency-safe, in-memory implementation
// keyed on task ID. Cap is the maximum token count allowed per task.
type InMemoryBudgetTracker struct {
	mu    sync.Mutex
	usage map[string]int
	cap   int
}

// NewBudgetTracker creates an in-memory budget tracker with the given per-task cap.
func NewBudgetTracker(tokenCap int) *InMemoryBudgetTracker {
	return &InMemoryBudgetTracker{
		usage: make(map[string]int),
		cap:   tokenCap,
	}
}

func (b *InMemoryBudgetTracker) Reserve(taskID string) error {
	if taskID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.usage[taskID] >= b.cap {
		return fmt.Errorf("%w: task %s used %d of %d allowed tokens",
			models.ErrBudgetExceeded, taskID, b.usage[taskID], b.cap)
	}
	return nil
}

func (b *InMemoryBudgetTracker) Add(taskID string, tokens int) {
	if taskID == "" || tokens <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usage[taskID] += tokens
}

func (b *InMemoryBudgetTracker) Usage(taskID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usage[taskID]
}

func (b *InMemoryBudgetTracker) Reset(taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.usage, taskID)
}
