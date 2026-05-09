package worker

import (
	"context"
	"sync"
)

type CancelRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewCancelRegistry() *CancelRegistry {
	return &CancelRegistry{cancels: make(map[string]context.CancelFunc)}
}

func (c *CancelRegistry) Register(taskID string, cancel context.CancelFunc) {
	if c == nil || taskID == "" || cancel == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[taskID] = cancel
}

func (c *CancelRegistry) Deregister(taskID string) {
	if c == nil || taskID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancels, taskID)
}

func (c *CancelRegistry) Cancel(taskID string) bool {
	if c == nil || taskID == "" {
		return false
	}
	c.mu.Lock()
	cancel, ok := c.cancels[taskID]
	c.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}
