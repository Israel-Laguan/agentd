package safety

import "context"

type Semaphore struct {
	ch chan struct{}
}

func NewSemaphore(limit int) *Semaphore {
	if limit < 1 {
		limit = 1
	}
	return &Semaphore{ch: make(chan struct{}, limit)}
}

func (s *Semaphore) Acquire(ctx context.Context) bool {
	select {
	case s.ch <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Semaphore) Release() {
	select {
	case <-s.ch:
	default:
	}
}

func (s *Semaphore) InUse() int { return len(s.ch) }

func (s *Semaphore) Capacity() int { return cap(s.ch) }

func (s *Semaphore) Available() int { return s.Capacity() - s.InUse() }
