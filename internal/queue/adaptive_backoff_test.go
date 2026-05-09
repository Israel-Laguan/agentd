package queue

import (
	"testing"
	"time"
)

func TestNextDispatchDelayDoublesOnEmpty(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		TaskInterval:    time.Second,
		MaxTaskInterval: 8 * time.Second,
		Probe:           StaticPIDProbe{},
	})

	delay := daemon.taskInterval
	expected := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		8 * time.Second,
	}
	for i, want := range expected {
		delay = daemon.nextDispatchDelay(delay, 0)
		if delay != want {
			t.Fatalf("step %d: delay = %s, want %s", i, delay, want)
		}
	}
}

func TestNextDispatchDelayResetsOnClaim(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		TaskInterval:    time.Second,
		MaxTaskInterval: 16 * time.Second,
		Probe:           StaticPIDProbe{},
	})

	delay := daemon.taskInterval
	delay = daemon.nextDispatchDelay(delay, 0)
	delay = daemon.nextDispatchDelay(delay, 0)
	if delay != 4*time.Second {
		t.Fatalf("after two empty polls: delay = %s, want 4s", delay)
	}

	delay = daemon.nextDispatchDelay(delay, 3)
	if delay != daemon.taskInterval {
		t.Fatalf("after claim: delay = %s, want base %s", delay, daemon.taskInterval)
	}
}

func TestNextDispatchDelayCapsAtMax(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		TaskInterval:    time.Second,
		MaxTaskInterval: 3 * time.Second,
		Probe:           StaticPIDProbe{},
	})

	delay := daemon.taskInterval
	for range 10 {
		delay = daemon.nextDispatchDelay(delay, 0)
	}
	if delay != 3*time.Second {
		t.Fatalf("after 10 empty polls: delay = %s, want cap at 3s", delay)
	}
}
