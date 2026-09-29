//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// diskHandoffTitle mirrors the constant of the same name in
// internal/queue/disk_watchdog.go. Duplicated rather than imported because the
// e2e package intentionally does not import internal/ — see
// test/e2e/client.go's chatMessage for the same reasoning.
const diskHandoffTitle = "Disk space critical. Please run cleanup or expand storage."

// diskSpaceCriticalEventType is the event the watchdog emits exactly once per
// created task (internal/queue/disk_watchdog.go emits only when
// EnsureProjectTask reports created == true), so it doubles as the dedup
// assertion's evidence.
const diskSpaceCriticalEventType = "DISK_SPACE_CRITICAL"

// TestJ10_DiskWatchdogDedup tests J10: disk below threshold raises exactly one
// HUMAN "Disk space critical" task, deduped on the next watchdog pass.
//
// The disk profile (agentd-disk on :8768) sets disk.free_threshold_percent to
// 100 (see devenv/agentd/config.disk.yaml), so the watchdog always alerts on
// the container's overlay filesystem. That is deliberate: J10 tests that the
// alert is raised once and deduped, not that a real disk-full condition
// occurs, and filling a real filesystem to 100% is neither possible nor
// desirable in a test.
//
// The watchdog's cadence is NOT a config key — there is no
// disk.check_interval. It comes from <home>/agentd.crontab
// (internal/config/cron.go's applyCronJob), which the compose service
// bind-mounts from devenv/agentd/crontab.disk with an "@every 5s
// disk-watchdog" entry, shortening the default */10 schedule. An earlier
// revision of config.disk.yaml set a `check_interval: 5s` key that no Go
// config struct reads; the watchdog silently stayed on */10 and this journey
// would have needed up to 10 minutes per pass.
//
// The task lands in the `_system` project (internal/kanban/system_project.go's
// fixed systemProjectID), which GET /api/v1/projects hides unless
// include_system=true — but it is addressable directly by ID, which is how
// this test observes it. Dedup itself is EnsureProjectTask's
// (project, title, assignee, not-terminal) match, so a second pass finds the
// open task, creates nothing, and emits no second event.
func TestJ10_DiskWatchdogDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Long enough for several 5s watchdog passes: the first raises the task,
	// the rest must be silent.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := NewHarness(diskBaseURL, "disk")
	if err := harness.WaitForHealthy(ctx, 15*time.Second); err != nil {
		t.Fatalf("J10 [boot] disk profile not healthy on %s: %v", diskBaseURL, err)
	}
	client := NewAPIClient(diskBaseURL, harness.client)

	task := j10AwaitDiskTask(ctx, t, client)

	// The watchdog's first pass may have run before the test connected, so
	// the event could predate this test's observation window. Assert on the
	// durable end state instead: exactly one matching task, carrying exactly
	// one DISK_SPACE_CRITICAL event, still present after several more passes.
	j10AssertSingleEvent(ctx, t, client, task.ID)

	passes := 3
	t.Logf("J10: task %s raised; waiting out %d further watchdog passes (~%s) to prove dedup",
		task.ID, passes, time.Duration(passes)*5*time.Second)
	time.Sleep(time.Duration(passes) * 5 * time.Second)

	matches := j10DiskTasks(ctx, t, client)
	if len(matches) != 1 {
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		t.Fatalf("J10 [dedup] found %d disk-critical tasks after %d extra watchdog passes, want 1 (ids: %s)",
			len(matches), passes, strings.Join(ids, ", "))
	}
	if matches[0].ID != task.ID {
		t.Fatalf("J10 [dedup] the surviving task is %s, but the first observed was %s — "+
			"the watchdog re-created instead of deduping", matches[0].ID, task.ID)
	}
	j10AssertSingleEvent(ctx, t, client, task.ID)

	t.Logf("J10: exactly one HUMAN disk task %s after %d passes, one %s event (dedup verified)",
		task.ID, passes+1, diskSpaceCriticalEventType)
}

// j10AwaitDiskTask polls the `_system` project's tasks until the disk
// watchdog's HUMAN task appears.
func j10AwaitDiskTask(ctx context.Context, t *testing.T, client *APIClient) *Task {
	t.Helper()

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if matches := j10DiskTasks(ctx, t, client); len(matches) > 0 {
			return &matches[0]
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J10 [watchdog] no %q task in _system within 45s — check that the disk profile's "+
		"crontab is bind-mounted with an \"@every 5s disk-watchdog\" entry (devenv/agentd/crontab.disk); "+
		"the default schedule is */10, which this journey would wait out", diskHandoffTitle)
	return nil
}

// j10DiskTasks returns the `_system` tasks matching the disk watchdog's title.
// The title is HUMAN-assigned but is not the "Manual review required:"
// prefix, so it is listed by default (internal/models.IsSelfHealingHandoffTask
// filters on that prefix) — include_healing is passed regardless so the
// assertion is robust to that predicate changing.
func j10DiskTasks(ctx context.Context, t *testing.T, client *APIClient) []Task {
	t.Helper()

	tasks, err := client.SystemProjectTasks(ctx, true)
	if err != nil {
		t.Fatalf("J10 [system tasks] %v", err)
	}
	var matches []Task
	for _, task := range tasks {
		if task.Title == diskHandoffTitle {
			matches = append(matches, task)
		}
	}
	return matches
}

// j10AssertSingleEvent asserts the watchdog emitted exactly one
// DISK_SPACE_CRITICAL event on the task. The event is emitted only when the
// task is newly created (internal/queue/disk_watchdog.go), so a second event
// would mean dedup failed at the event layer even if the task count held.
func j10AssertSingleEvent(ctx context.Context, t *testing.T, client *APIClient, taskID string) {
	t.Helper()

	events, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("J10 [events] %v", err)
	}
	count := 0
	for _, e := range events {
		if e.Type == diskSpaceCriticalEventType {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("J10 [events] task %s has %d %s events, want exactly 1 (%s)",
			taskID, count, diskSpaceCriticalEventType, fmt.Sprint(events))
	}
}
