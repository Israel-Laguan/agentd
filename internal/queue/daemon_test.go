package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/testutil"
)

func TestDaemon_Stop(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{Probe: StaticPIDProbe{}})
	if err := daemon.Stop(); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
}

func TestSchedulerLoop_ExitsWhenContextCanceled(t *testing.T) {
	store := testutil.NewFakeStore()
	sched := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		Scheduler:             sched,
		SchedulerTickInterval: time.Hour,
		Probe:                 StaticPIDProbe{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	daemon.wg.Add(1)
	done := make(chan struct{})
	go func() {
		daemon.schedulerLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("schedulerLoop did not exit after cancel")
	}
}

func TestSchedulerLoop_DisabledSchedulerReturnsImmediately(t *testing.T) {
	store := testutil.NewFakeStore()
	sched := NewScheduler(store, nil, SchedulerOptions{Enabled: false})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		Scheduler: sched,
		Probe:     StaticPIDProbe{},
	})
	ctx := context.Background()
	daemon.wg.Add(1)
	done := make(chan struct{})
	go func() {
		daemon.schedulerLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("schedulerLoop did not return for disabled scheduler")
	}
}

func TestSchedulerLoop_TicksBeforeCancel(t *testing.T) {
	store := testutil.NewFakeStore()
	sched := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		Scheduler:             sched,
		SchedulerTickInterval: 5 * time.Millisecond,
		Probe:                 StaticPIDProbe{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	daemon.wg.Add(1)
	go daemon.schedulerLoop(ctx)
	time.Sleep(15 * time.Millisecond)
	cancel()
	waitDone := make(chan struct{})
	go func() {
		daemon.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("schedulerLoop wg did not complete after cancel")
	}
}

func TestDaemonStart_CancelsAllLoops(t *testing.T) {
	store := testutil.NewFakeStore()
	sched := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	daemon := NewDaemon(store, nil, nil, nil, nil, DaemonOptions{
		TaskInterval:          time.Hour,
		MaxTaskInterval:       time.Hour,
		IntakeInterval:        time.Hour,
		HeartbeatInterval:     time.Hour,
		QueuedReconcileAfter:  0,
		Probe:                 StaticPIDProbe{},
		Scheduler:             sched,
		SchedulerTickInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

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
