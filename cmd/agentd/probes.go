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

// Reset implements services.BreakerResetter for operator-initiated recovery.
func (p breakerProbe) Reset() {
	if p.breaker != nil {
		p.breaker.Reset()
	}
}

var _ services.BreakerResetter = breakerProbe{}

// providerBreakersProbe adapts *queue.ProviderBreakers to services.ProviderBreakersProbe.
type providerBreakersProbe struct {
	pb *queue.ProviderBreakers
}

var _ services.ProviderBreakersProbe = providerBreakersProbe{}

func (p providerBreakersProbe) Snapshot() map[string]services.ProviderBreakerEntry {
	if p.pb == nil {
		return nil
	}
	raw := p.pb.Snapshot()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]services.ProviderBreakerEntry, len(raw))
	for name, e := range raw {
		out[name] = services.ProviderBreakerEntry{
			State:        e.State,
			FailureCount: e.FailureCount,
			OpenFor:      e.OpenFor,
			LastError:    e.LastError,
		}
	}
	return out
}

func (p providerBreakersProbe) Reset(provider string) {
	if p.pb != nil {
		p.pb.Reset(provider)
	}
}

func (p providerBreakersProbe) ResetAll() {
	if p.pb != nil {
		p.pb.ResetAll()
	}
}

// rollingBudgetProbe adapts *queue.RollingTokenLedger to services.RollingBudgetProbe.
type rollingBudgetProbe struct {
	ledger *queue.RollingTokenLedger
	window time.Duration
}

var _ services.RollingBudgetProbe = rollingBudgetProbe{}

func (p rollingBudgetProbe) RollingBudgetSnapshot() (limit, remaining int, enabled bool, window time.Duration) {
	window = p.window
	if p.ledger == nil || !p.ledger.Enabled() {
		return 0, 0, false, window
	}
	limit = p.ledger.Limit()
	return limit, p.ledger.BudgetRemaining(limit), true, window
}
