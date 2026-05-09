package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/sandbox"
)

func TestDispatchConcurrencyCapIsRespected(t *testing.T) {
	const (
		totalTasks = 20
		maxSlots   = 4
		taskSleep  = 50 * time.Millisecond
	)
	store := newQueueStore()
	store.seed(totalTasks, "READY")

	var peak, current atomic.Int32
	wrappedSb := &concurrencyTrackingSandbox{
		inner:   &fakeSandbox{result: sandbox.Result{Success: true, ExitCode: 0}, delay: taskSleep},
		current: &current,
		peak:    &peak,
	}

	gw := &queueGateway{content: `{"command":"true"}`}
	breaker := NewCircuitBreaker()
	sink := &queueSink{}
	worker := NewWorker(store, gw, wrappedSb, breaker, sink, WorkerOptions{})

	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{
		MaxWorkers: maxSlots, TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})

	start := time.Now()
	dispatchAll(t, daemon, store)
	waitSemaphore(t, daemon.sem, maxSlots, 30*time.Second)
	elapsed := time.Since(start)

	if p := peak.Load(); p > int32(maxSlots) {
		t.Fatalf("peak concurrency = %d, must not exceed %d", p, maxSlots)
	}
	expectedMin := time.Duration(totalTasks/maxSlots) * taskSleep
	if elapsed < expectedMin/2 {
		t.Fatalf("elapsed = %s, suspiciously fast (expected >= ~%s)", elapsed, expectedMin)
	}
}

func dispatchAll(t *testing.T, daemon *Daemon, store *queueStore) {
	t.Helper()
	for store.count("READY")+store.count("QUEUED") > 0 {
		claimed, err := daemon.dispatch(context.Background())
		if err != nil {
			t.Fatalf("dispatch() error = %v", err)
		}
		if claimed == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func waitSemaphore(t *testing.T, sem *Semaphore, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for sem.Available() != want {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for semaphore (available=%d, want=%d)", sem.Available(), want)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

type concurrencyTrackingSandbox struct {
	inner   sandbox.Executor
	current *atomic.Int32
	peak    *atomic.Int32
}

func (s *concurrencyTrackingSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	c := s.current.Add(1)
	for {
		p := s.peak.Load()
		if c <= p || s.peak.CompareAndSwap(p, c) {
			break
		}
	}
	defer s.current.Add(-1)
	return s.inner.Execute(ctx, payload)
}
