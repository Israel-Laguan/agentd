package main

import (
	"errors"
	"testing"
	"time"

	"agentd/internal/queue"
)

func TestBreakerProbeNilBreaker(t *testing.T) {
	p := breakerProbe{breaker: nil}
	if p.State() != "" || p.FailureCount() != 0 || p.OpenDuration() != 0 || p.LastError() != nil {
		t.Fatalf("nil breaker probe = %#v %#v %#v %v", p.State(), p.FailureCount(), p.OpenDuration(), p.LastError())
	}
}

func TestBreakerProbeDelegates(t *testing.T) {
	b := queue.NewCircuitBreaker()
	now := time.Unix(1700, 0).UTC()
	b.ArmForResilienceTest(now, now.Add(-time.Minute), 2, errors.New("boom"))
	p := breakerProbe{breaker: b}
	if p.State() != "OPEN" {
		t.Fatalf("state = %q", p.State())
	}
	if p.FailureCount() != 2 || p.OpenDuration() <= 0 {
		t.Fatalf("counts: fc=%d open=%v", p.FailureCount(), p.OpenDuration())
	}
	if p.LastError() == nil || p.LastError().Error() != "boom" {
		t.Fatalf("last err = %v", p.LastError())
	}
}
