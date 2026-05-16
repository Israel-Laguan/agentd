package queue

import (
	"testing"
	"time"
)

func TestHITLReconcileDelayUsesEveryWhenConfigured(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers:         1,
		HITLReconcileEvery: 25 * time.Millisecond,
	})

	if got := daemon.nextHITLReconcileDelay(time.Now()); got != 25*time.Millisecond {
		t.Fatalf("nextHITLReconcileDelay() = %s, want 25ms", got)
	}
}
