package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
)

// runningCounter counts the tasks the store holds in RUNNING, which is the
// concurrency a dispatch actually achieves: a task is RUNNING from the moment a
// worker claims it until it records a result.
type runningCounter struct {
	*queueStore
	mu      sync.Mutex
	current int
	peak    int
	claimed map[string]bool
}

func (c *runningCounter) MarkTaskRunning(ctx context.Context, id string, at time.Time, pid int) (*models.Task, error) {
	task, err := c.queueStore.MarkTaskRunning(ctx, id, at, pid)
	if err != nil {
		return task, err
	}
	c.mu.Lock()
	if c.claimed == nil {
		c.claimed = map[string]bool{}
	}
	if !c.claimed[id] {
		c.claimed[id] = true
		c.current++
		if c.current > c.peak {
			c.peak = c.current
		}
	}
	c.mu.Unlock()
	return task, nil
}

// leave drops one claimed task, once. Failure paths commit a result and then
// make a second state transition, so leaving per call would decrement twice
// for one claim and underreport peak overlap.
func (c *runningCounter) leave(id string) {
	c.mu.Lock()
	if c.claimed[id] {
		delete(c.claimed, id)
		c.current--
	}
	c.mu.Unlock()
}

func (c *runningCounter) UpdateTaskResult(ctx context.Context, id string, at time.Time, result models.TaskResult) (*models.Task, error) {
	defer c.leave(id)
	return c.queueStore.UpdateTaskResult(ctx, id, at, result)
}

func (c *runningCounter) UpdateTaskState(ctx context.Context, id string, at time.Time, next models.TaskState) (*models.Task, error) {
	defer c.leave(id)
	return c.queueStore.UpdateTaskState(ctx, id, at, next)
}

func (c *runningCounter) peakRunning() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak
}

func TestDispatchConcurrencyCapIsRespected(t *testing.T) {
	const (
		totalTasks = 20
		maxSlots   = 4
		taskSleep  = 50 * time.Millisecond
	)
	base := newQueueStore()
	store := &runningCounter{queueStore: base}
	store.seed(totalTasks, "READY")
	for i := range store.tasks {
		store.tasks[i].Title = fakeagent.Script(fakeagent.Step{Message: "working"}, fakeagent.Step{SleepMS: int(taskSleep.Milliseconds())}, acpReport("done"))
	}

	acp, closeACP := newACPRuntime()
	t.Cleanup(closeACP)
	sink := &queueSink{}
	worker := NewWorker(store, &fakeGateway{}, nil, NewCircuitBreaker(), sink,
		WorkerOptions{Routes: acpQueueRoutes(), ACP: acp})

	daemon := NewDaemon(store, worker, nil, NewCircuitBreaker(), sink, DaemonOptions{
		MaxWorkers: maxSlots, TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})

	start := time.Now()
	dispatchAll(t, daemon, store)
	waitSemaphore(t, daemon.sem, maxSlots, 30*time.Second)
	elapsed := time.Since(start)

	if p := store.peakRunning(); p > maxSlots {
		t.Fatalf("peak concurrency = %d, must not exceed %d", p, maxSlots)
	}
	expectedMin := time.Duration(totalTasks/maxSlots) * taskSleep
	if elapsed < expectedMin/2 {
		t.Fatalf("elapsed = %s, suspiciously fast (expected >= ~%s)", elapsed, expectedMin)
	}
}

func dispatchAll(t *testing.T, daemon *Daemon, store *runningCounter) {
	t.Helper()
	for store.count("READY")+store.count("QUEUED") > 0 {
		dispatched, _, err := daemon.dispatch(context.Background())
		if err != nil {
			t.Fatalf("dispatch() error = %v", err)
		}
		if dispatched == 0 {
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
