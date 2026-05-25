package safety

import (
	"sync"
	"time"
)

// ProviderBreakers maintains one CircuitBreaker per named provider.
// It is nil-safe: methods on a nil *ProviderBreakers are no-ops/zero-returns.
type ProviderBreakers struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
}

// NewProviderBreakers returns an empty, ready-to-use ProviderBreakers registry.
func NewProviderBreakers() *ProviderBreakers {
	return &ProviderBreakers{breakers: make(map[string]*CircuitBreaker)}
}

// Get returns the CircuitBreaker for the given provider, creating one on first use.
// An empty provider name returns a throwaway breaker so callers never get nil.
func (p *ProviderBreakers) Get(provider string) *CircuitBreaker {
	if p == nil {
		return NewCircuitBreaker()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if b, ok := p.breakers[provider]; ok {
		return b
	}
	b := NewCircuitBreaker()
	p.breakers[provider] = b
	return b
}

// Reset resets the breaker for the named provider.
// It is a no-op when the provider has no recorded state.
func (p *ProviderBreakers) Reset(provider string) {
	if p == nil {
		return
	}
	p.mu.RLock()
	b := p.breakers[provider]
	p.mu.RUnlock()
	if b != nil {
		b.Reset()
	}
}

// ResetAll resets every known provider breaker.
func (p *ProviderBreakers) ResetAll() {
	if p == nil {
		return
	}
	p.mu.RLock()
	all := make([]*CircuitBreaker, 0, len(p.breakers))
	for _, b := range p.breakers {
		all = append(all, b)
	}
	p.mu.RUnlock()
	for _, b := range all {
		b.Reset()
	}
}

// ProviderBreakerEntry is the snapshot of one provider's breaker state.
type ProviderBreakerEntry struct {
	State        string
	FailureCount int
	OpenFor      time.Duration
	LastError    string
}

// Snapshot returns a copy of each provider's breaker state keyed by provider name.
func (p *ProviderBreakers) Snapshot() map[string]ProviderBreakerEntry {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]ProviderBreakerEntry, len(p.breakers))
	for name, b := range p.breakers {
		entry := ProviderBreakerEntry{
			State:        string(b.State()),
			FailureCount: b.FailureCount(),
			OpenFor:      b.OpenDuration(),
		}
		if err := b.LastError(); err != nil {
			entry.LastError = err.Error()
		}
		out[name] = entry
	}
	return out
}
