package main

import (
	"time"

	"agentd/internal/queue"
	"agentd/internal/services"
)

// breakerProbe adapts *queue.CircuitBreaker to services.BreakerProbe.
// The adapter exists because queue.BreakerState is a typed string while
// the services interface uses plain string to stay decoupled from
// internal/queue.
type breakerProbe struct {
	breaker *queue.CircuitBreaker
}

var _ services.BreakerProbe = breakerProbe{}

func (p breakerProbe) State() string {
	if p.breaker == nil {
		return ""
	}
	return string(p.breaker.State())
}

func (p breakerProbe) FailureCount() int {
	if p.breaker == nil {
		return 0
	}
	return p.breaker.FailureCount()
}

func (p breakerProbe) OpenDuration() time.Duration {
	if p.breaker == nil {
		return 0
	}
	return p.breaker.OpenDuration()
}

func (p breakerProbe) LastError() error {
	if p.breaker == nil {
		return nil
	}
	return p.breaker.LastError()
}
