package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestCountTasksByStateGroupsPerProject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	a, aTasks := materializeCounts(t, store, "counts-a", []models.DraftTask{
		{TempID: "x", Title: "x"}, {TempID: "y", Title: "y"}, {TempID: "z", Title: "z", DependsOn: []string{"x"}},
	})
	b, _ := materializeCounts(t, store, "counts-b", []models.DraftTask{{TempID: "x", Title: "x"}})
	if _, _, err := store.BlockTaskWithSubtasks(ctx, aTasks[0].ID, aTasks[0].UpdatedAt, []models.DraftTask{{
		Title: models.HITLSubtaskTitleManualReview + " outage", Assignee: models.TaskAssigneeHuman,
	}}); err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}

	with := countsByKey(t, store, true)
	if with[a.ID+"/READY"] != 2 || with[a.ID+"/PENDING"] != 1 || with[a.ID+"/BLOCKED"] != 1 || with[b.ID+"/READY"] != 1 {
		t.Fatalf("counts with healing = %v", with)
	}
	without := countsByKey(t, store, false)
	if without[a.ID+"/READY"] != 1 || without[a.ID+"/BLOCKED"] != 1 || without[b.ID+"/READY"] != 1 {
		t.Fatalf("counts without healing = %v, want the manual-review handoff excluded", without)
	}
}

func TestListAttentionTasksSkipsPlainWork(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks := materializeCounts(t, store, "attn", []models.DraftTask{
		{TempID: "x", Title: "plain"}, {TempID: "y", Title: "human", Assignee: models.TaskAssigneeHuman},
	})
	got, err := store.ListAttentionTasks(ctx)
	if err != nil {
		t.Fatalf("ListAttentionTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID == tasks[0].ID || got[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("attention tasks = %+v, want only the human-assigned task", got)
	}
}

func materializeCounts(t *testing.T, store *Store, name string, drafts []models.DraftTask) (*models.Project, []models.Task) {
	t.Helper()
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{ProjectName: name, Tasks: drafts})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	return project, tasks
}

func countsByKey(t *testing.T, store *Store, includeHealing bool) map[string]int {
	t.Helper()
	rows, err := store.CountTasksByState(context.Background(), includeHealing)
	if err != nil {
		t.Fatalf("CountTasksByState: %v", err)
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.ProjectID+"/"+string(r.State)] = r.Count
	}
	return out
}
