package bus

import (
	"context"
	"sync"
)

type subscriber struct {
	ch chan Signal
}

// InProcess is a process-local Bus implementation.
type InProcess struct {
	mu          sync.RWMutex
	subscribers map[string]map[*subscriber]struct{}
}

var _ Bus = (*InProcess)(nil)

// NewInProcess creates an in-memory event bus.
func NewInProcess() *InProcess {
	return &InProcess{subscribers: make(map[string]map[*subscriber]struct{})}
}

// Publish sends a signal to current subscribers without blocking the caller.
// Slow subscribers may miss signals; persistence remains the source of truth.
func (m *InProcess) Publish(_ context.Context, sig Signal) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for sub := range m.subscribers[sig.Topic] {
		select {
		case sub.ch <- sig:
		default:
		}
	}
}

// Subscribe registers a buffered subscriber for one topic.
func (m *InProcess) Subscribe(topic string, buffer int) (<-chan Signal, func()) {
	if buffer < 1 {
		buffer = 1
	}
	sub := &subscriber{ch: make(chan Signal, buffer)}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.subscribers[topic] == nil {
		m.subscribers[topic] = make(map[*subscriber]struct{})
	}
	m.subscribers[topic][sub] = struct{}{}
	return sub.ch, func() { m.unsubscribe(topic, sub) }
}

func (m *InProcess) unsubscribe(topic string, sub *subscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.subscribers[topic][sub]; !ok {
		return
	}
	delete(m.subscribers[topic], sub)
	close(sub.ch)
	if len(m.subscribers[topic]) == 0 {
		delete(m.subscribers, topic)
	}
}
