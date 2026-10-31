//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// j23TitlePrefix is the title of the j23-once entry in
// devenv/agentd/config.scheduler.yaml. The scheduler appends the slot, so the
// created task is "J23 scheduled once [2020-01-01 00:00]".
const j23TitlePrefix = "J23 scheduled once"

// TestJ23_ScheduledTaskRunsExactlyOnce tests J23 (T-058, B-076): a one-shot
// scheduled task with a real project_id reaches COMPLETED, and neither more
// ticks nor a daemon restart fire it again.
//
// The scheduler profile (agentd-sched on :8772) boots with the scheduler
// disabled: an enabled scheduler whose task resolves to no project fails
// config load (B-076), and a project ID exists only once the project does. So
// the journey creates the project, writes the scheduler's enabled flag and
// project_id to <home>/.env, and restarts the daemon. The second restart
// checks that last_fired_at survives it.
func TestJ23_ScheduledTaskRunsExactlyOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	harness := NewHarness(schedulerBaseURL, "scheduler")
	if err := harness.WaitForHealthy(ctx, 30*time.Second); err != nil {
		t.Fatalf("J23 [boot] scheduler profile not healthy on %s: %v", schedulerBaseURL, err)
	}
	client := NewAPIClient(schedulerBaseURL, harness.client)
	devenv := NewDevenvManager(composePath, "scheduler")
	j23Reset(ctx, t, devenv, harness)

	projectID := materializePlan(ctx, t, client, "J23", DraftPlan{
		ProjectName:         UniqueProjectName("j23"),
		Description:         "J23 scheduler target project",
		StartEmptyWorkspace: true,
		Tasks:               []DraftTask{{Title: "J23 seed task", Description: "Give the project a first task."}},
	}).Project.ID

	env := fmt.Sprintf("AGENTD_AGENTIC_SCHEDULER_ENABLED=true\nAGENTD_AGENTIC_SCHEDULER_PROJECT_ID=%s\n", projectID)
	script := fmt.Sprintf("printf '%%s' '%s' > %s/.env", env, profileService["scheduler"].home)
	if _, err := devenv.Exec(ctx, "sh", "-c", script); err != nil {
		t.Fatalf("J23 [env] write .env: %v", err)
	}
	j23Restart(ctx, t, devenv, harness, "enable")

	poller := NewTaskPoller(client, projectID)
	task := j23AwaitScheduledTask(ctx, t, poller)
	if _, err := poller.WaitForTaskState(ctx, task.ID, TaskStateCompleted, 90*time.Second); err != nil {
		t.Fatalf("J23 [run] scheduled task %s did not reach COMPLETED: %v", task.ID, err)
	}

	// Restart once more, then wait out at least five 2 s ticks: a one-shot
	// whose last_fired_at was lost would fire again in that window.
	j23Restart(ctx, t, devenv, harness, "re-fire check")
	const window = 15 * time.Second
	time.Sleep(window)

	matches := j23ScheduledTasks(ctx, t, poller)
	if len(matches) != 1 || matches[0].ID != task.ID {
		t.Fatalf("J23 [exactly once] found %d scheduled tasks after a restart and %s of ticks, want only %s: %+v",
			len(matches), window, task.ID, matches)
	}
	t.Logf("J23: scheduled task %s ran to COMPLETED once; no re-fire after a restart", task.ID)
}

// j23Reset starts the journey from an empty board and no .env. A fired one-shot
// keeps its row and last_fired_at (B-088), so on a volume reused from an
// earlier run j23-once would never fire again. The .env goes too: it names the
// earlier run's project, which the empty board does not have.
func j23Reset(ctx context.Context, t *testing.T, devenv *DevenvManager, harness *Harness) {
	t.Helper()
	home := profileService["scheduler"].home
	script := fmt.Sprintf("rm -f %s/.env %s/global.db %s/global.db-wal %s/global.db-shm", home, home, home, home)
	if _, err := devenv.Exec(ctx, "sh", "-c", script); err != nil {
		t.Fatalf("J23 [reset] clear the board: %v", err)
	}
	j23Restart(ctx, t, devenv, harness, "reset")
}

// j23Restart kills and restarts the scheduler profile's daemon; its volume, the
// board and <home>/.env survive.
func j23Restart(ctx context.Context, t *testing.T, devenv *DevenvManager, harness *Harness, step string) {
	t.Helper()
	if err := devenv.KillAgentd(ctx); err != nil {
		t.Fatalf("J23 [%s] kill: %v", step, err)
	}
	if err := devenv.RestartAgentd(ctx); err != nil {
		t.Fatalf("J23 [%s] restart: %v", step, err)
	}
	if err := harness.WaitForHealthy(ctx, 60*time.Second); err != nil {
		t.Fatalf("J23 [%s] daemon not healthy after restart (did the .env fail config load?): %v", step, err)
	}
}

// j23AwaitScheduledTask polls the project until the scheduler's task appears.
func j23AwaitScheduledTask(ctx context.Context, t *testing.T, poller *TaskPoller) Task {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if matches := j23ScheduledTasks(ctx, t, poller); len(matches) > 0 {
			return matches[0]
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J23 [schedule] no %q task within 30s of enabling the scheduler (tick_interval 2s)", j23TitlePrefix)
	return Task{}
}

// j23ScheduledTasks returns the project's tasks created by the j23-once entry.
func j23ScheduledTasks(ctx context.Context, t *testing.T, poller *TaskPoller) []Task {
	t.Helper()
	tasks, err := poller.listTasks(ctx, true)
	if err != nil {
		t.Fatalf("J23 [list tasks] %v", err)
	}
	var matches []Task
	for _, task := range tasks {
		if strings.HasPrefix(task.Title, j23TitlePrefix) {
			matches = append(matches, task)
		}
	}
	return matches
}
