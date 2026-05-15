package testutil

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"agentd/internal/models"
)

func exerciseStoreSetup(t *testing.T) (context.Context, *FakeKanbanStore, *models.Project, []models.Task) {
	t.Helper()
	ctx := context.Background()
	s := NewFakeStore()
	if _, err := s.EnsureSystemProject(ctx); err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	plan := models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{
			{Title: "T1", Assignee: models.TaskAssigneeSystem},
			{Title: "T2", Assignee: models.TaskAssigneeSystem},
		},
	}
	proj, tasks, err := s.MaterializePlan(ctx, plan)
	if err != nil || len(tasks) < 2 {
		t.Fatalf("MaterializePlan: %v %d", err, len(tasks))
	}
	return ctx, s, proj, tasks
}

func TestFakeKanbanStore_ProjectsAndTasks(t *testing.T) {
	ctx, s, proj, _ := exerciseStoreSetup(t)
	gotProj, err := s.GetProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if gotProj.ID != proj.ID {
		t.Fatal("project mismatch")
	}
	list, err := s.ListProjects(ctx)
	if err != nil || len(list) < 1 {
		t.Fatalf("ListProjects: %v %d", err, len(list))
	}
	_, created, err := s.EnsureProjectTask(ctx, proj.ID, models.DraftTask{Title: "T2"})
	if err != nil || created {
		t.Fatalf("EnsureProjectTask dup: created=%v err=%v", created, err)
	}
	_, created, err = s.EnsureProjectTask(ctx, proj.ID, models.DraftTask{Title: "new"})
	if err != nil || !created {
		t.Fatalf("EnsureProjectTask new: %v created=%v", err, created)
	}
	byProj, err := s.ListTasksByProject(ctx, proj.ID)
	if err != nil || len(byProj) < 1 {
		t.Fatalf("ListTasksByProject: %v", err)
	}
}

func TestFakeKanbanStore_TaskLifecycle(t *testing.T) {
	ctx, s, proj, tasks := exerciseStoreSetup(t)
	runBasicTaskLifecycle(t, ctx, s)

	runningID := tasks[1].ID
	rt, err := s.GetTask(ctx, runningID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.MarkTaskRunning(ctx, runningID, rt.UpdatedAt, 5555)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	rt2, err := s.GetTask(ctx, runningID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.UpdateTaskState(ctx, runningID, rt2.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	rt3, err := s.GetTask(ctx, runningID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, subs, err := s.BlockTaskWithSubtasks(ctx, runningID, rt3.UpdatedAt, []models.DraftTask{{Title: "sub"}})
	if err != nil || len(subs) != 1 {
		t.Fatalf("BlockTaskWithSubtasks: %v %#v", err, subs)
	}
	_, err = s.AppendTasksToProject(ctx, proj.ID, runningID, []models.DraftTask{{Title: "appended"}})
	if err != nil {
		t.Fatalf("AppendTasksToProject: %v", err)
	}
}

func runBasicTaskLifecycle(t *testing.T, ctx context.Context, s *FakeKanbanStore) {
	t.Helper()
	claimed, err := s.ClaimNextReadyTasks(ctx, 10)
	if err != nil || len(claimed) < 1 {
		t.Fatalf("ClaimNextReadyTasks: %v", err)
	}
	q := claimed[0]
	_, err = s.MarkTaskRunning(ctx, q.ID, q.UpdatedAt, 4242)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	if err := s.UpdateTaskHeartbeat(ctx, q.ID); err != nil {
		t.Fatalf("UpdateTaskHeartbeat: %v", err)
	}
	q2, err := s.GetTask(ctx, q.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.IncrementRetryCount(ctx, q.ID, q2.UpdatedAt)
	if err != nil {
		t.Fatalf("IncrementRetryCount: %v", err)
	}
	q3, err := s.GetTask(ctx, q.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.UpdateTaskState(ctx, q.ID, q3.UpdatedAt, models.TaskStateReady)
	if err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	q4, err := s.GetTask(ctx, q.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.UpdateTaskResult(ctx, q.ID, q4.UpdatedAt, models.TaskResult{Success: true})
	if err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	_, err = s.ReconcileGhostTasks(ctx, []int{4242})
	if err != nil {
		t.Fatalf("ReconcileGhostTasks: %v", err)
	}
	_, err = s.ReconcileStaleTasks(ctx, []int{9999}, time.Minute)
	if err != nil {
		t.Fatalf("ReconcileStaleTasks: %v", err)
	}
}

func TestFakeKanbanStore_CommentsAndEvents(t *testing.T) {
	ctx, s, proj, tasks := exerciseStoreSetup(t)
	commentTask := tasks[0].ID
	if err := s.AddComment(ctx, models.Comment{
		BaseEntity: models.BaseEntity{ID: "c1"},
		TaskID:     commentTask, Author: models.CommentAuthorUser, Body: "hello",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	comments, err := s.ListComments(ctx, commentTask)
	if err != nil || len(comments) < 1 {
		t.Fatalf("ListComments: %v", err)
	}
	if _, err := s.ListUnprocessedHumanComments(ctx); err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	if err := s.MarkCommentProcessed(ctx, commentTask, "x"); err != nil {
		t.Fatalf("MarkCommentProcessed: %v", err)
	}
	if err := s.AppendEvent(ctx, models.Event{
		BaseEntity: models.BaseEntity{ID: "e1"},
		ProjectID:  proj.ID,
		TaskID:     sql.NullString{String: commentTask, Valid: true},
		Type:       models.EventTypeComment,
		Payload:    "x",
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	evs, err := s.ListEventsByTask(ctx, commentTask)
	if err != nil || len(evs) < 1 {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	if err := s.MarkEventsCurated(ctx, commentTask); err != nil {
		t.Fatalf("MarkEventsCurated: %v", err)
	}
	if err := s.DeleteCuratedEvents(ctx, commentTask); err != nil {
		t.Fatalf("DeleteCuratedEvents: %v", err)
	}
}

func TestFakeKanbanStore_Memory(t *testing.T) {
	ctx, s, _, _ := exerciseStoreSetup(t)
	if err := s.RecordMemory(ctx, models.Memory{ID: "m1", Scope: models.MemoryScopeGlobal}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	if _, err := s.ListMemories(ctx, models.MemoryFilter{Scope: models.MemoryScopeGlobal}); err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if _, err := s.RecallMemories(ctx, models.RecallQuery{Limit: 5}); err != nil {
		t.Fatalf("RecallMemories: %v", err)
	}
	if err := s.TouchMemories(ctx, []string{"m1"}); err != nil {
		t.Fatalf("TouchMemories: %v", err)
	}
	if err := s.SupersedeMemories(ctx, []string{"m1"}, "m2"); err != nil {
		t.Fatalf("SupersedeMemories: %v", err)
	}
	if _, err := s.ListUnsupersededMemories(ctx); err != nil {
		t.Fatalf("ListUnsupersededMemories: %v", err)
	}
}

func TestFakeKanbanStore_AgentsAndCleanup(t *testing.T) {
	ctx, s, _, tasks := exerciseStoreSetup(t)
	doneTask := tasks[1].ID
	dt, err := s.GetTask(ctx, doneTask)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	_, err = s.UpdateTaskResult(ctx, doneTask, dt.UpdatedAt, models.TaskResult{Success: true})
	if err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if _, err := s.ListCompletedTasksOlderThan(ctx, time.Nanosecond); err != nil {
		t.Fatalf("ListCompletedTasksOlderThan: %v", err)
	}
	if _, err := s.ListAgentProfiles(ctx); err != nil {
		t.Fatalf("ListAgentProfiles: %v", err)
	}
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := s.ListSettings(ctx); err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if v, ok, err := s.GetSetting(ctx, "k"); err != nil || !ok || v != "v" {
		t.Fatalf("GetSetting: %v %v %q", ok, err, v)
	}
	_ = s.Events()
	_ = s.Tasks()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
