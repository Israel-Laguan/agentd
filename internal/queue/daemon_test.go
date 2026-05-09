package queue

import (
	"testing"
	"time"
)

func TestNewDaemonUsesConfiguredIntervals(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		TaskInterval:      7 * time.Millisecond,
		IntakeInterval:    9 * time.Millisecond,
		HeartbeatInterval: 11 * time.Millisecond,
		Probe:             StaticPIDProbe{},
	})

	if daemon.taskInterval != 7*time.Millisecond {
		t.Fatalf("taskInterval = %s, want 7ms", daemon.taskInterval)
	}
	if daemon.intakeEvery != 9*time.Millisecond {
		t.Fatalf("intakeEvery = %s, want 9ms", daemon.intakeEvery)
	}
	if daemon.heartbeatInterval != 11*time.Millisecond {
		t.Fatalf("heartbeatInterval = %s, want 11ms", daemon.heartbeatInterval)
	}
}
