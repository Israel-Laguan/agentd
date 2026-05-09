package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestDiskWatchdogCreatesHumanTask(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers:        1,
		DiskFreeThreshold: 10,
		DiskCheckPath:     "/tmp/agentd-test",
	})
	daemon.diskStat = func(string) (float64, error) { return 5, nil }

	if err := daemon.checkDiskSpace(ctx); err != nil {
		t.Fatalf("checkDiskSpace() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "_system" {
		t.Fatalf("projects = %#v, want system project", projects)
	}
	tasks, err := store.ListTasksByProject(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("ListTasksByProject() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.Title != diskHandoffTitle || task.Assignee != models.TaskAssigneeHuman {
		t.Fatalf("task = %#v, want disk HUMAN task", task)
	}
	for _, want := range []string{"Disk free space is below", "Free space: 5.00%", "Threshold: 10.00%", "/tmp/agentd-test"} {
		if !strings.Contains(task.Description, want) {
			t.Fatalf("description missing %q:\n%s", want, task.Description)
		}
	}
	if len(sink.events) != 1 || sink.events[0].Type != "DISK_SPACE_CRITICAL" {
		t.Fatalf("events = %#v, want one DISK_SPACE_CRITICAL", sink.events)
	}
}

func TestDiskWatchdogDeduplicatesOpenTask(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers:        1,
		DiskFreeThreshold: 10,
		DiskCheckPath:     "/tmp/agentd-test",
	})
	daemon.diskStat = func(string) (float64, error) { return 5, nil }

	if err := daemon.checkDiskSpace(ctx); err != nil {
		t.Fatalf("first checkDiskSpace() error = %v", err)
	}
	if err := daemon.checkDiskSpace(ctx); err != nil {
		t.Fatalf("second checkDiskSpace() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	tasks, err := store.ListTasksByProject(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("ListTasksByProject() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want deduplicated single task", len(tasks))
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want one handoff event", len(sink.events))
	}
}

func TestDiskWatchdogNoAlertAboveThreshold(t *testing.T) {
	store := newQueueTestStore(t)
	ctx := context.Background()
	sink := &recordingSink{}
	daemon := NewDaemon(store, nil, nil, nil, sink, DaemonOptions{
		MaxWorkers:        1,
		DiskFreeThreshold: 10,
		DiskCheckPath:     "/tmp/agentd-test",
	})
	daemon.diskStat = func(string) (float64, error) { return 50, nil }

	if err := daemon.checkDiskSpace(ctx); err != nil {
		t.Fatalf("checkDiskSpace() error = %v", err)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("projects = %#v, want no system project", projects)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %#v, want none", sink.events)
	}
}

func TestDiskWatchdogDelayUsesEveryWhenConfigured(t *testing.T) {
	daemon := NewDaemon(nil, nil, nil, nil, nil, DaemonOptions{
		MaxWorkers:        1,
		DiskWatchdogEvery: 25 * time.Millisecond,
	})

	if got := daemon.nextDiskWatchdogDelay(time.Now()); got != 25*time.Millisecond {
		t.Fatalf("nextDiskWatchdogDelay() = %s, want 25ms", got)
	}
}
