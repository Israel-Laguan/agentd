package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/config"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
)

func TestTextBatchUsesEachTasksACPRoute(t *testing.T) {
	tasks := summarizeTasks(2)
	for i := range tasks {
		tasks[i].AgentID = route.DefaultAgentID
		tasks[i].RequiresWorkspaceChange = false
		tasks[i].RequiresWorkspaceChangeClassified = true
		tasks[i].Title = fakeagent.Script(report(map[string]any{"summary": "ACP completed"}))
	}
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: t.TempDir()},
		profile: models.AgentProfile{ID: route.DefaultAgentID},
		tasks:   make(map[string]models.Task), results: make(map[string]*models.TaskResult),
	}
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	rt, _, log := acpRuntime(t)
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, nil, WorkerOptions{Routes: acpRoutes(), ACP: rt, Batching: config.BatchingConfig{Enabled: true}})
	w.ProcessBatch(context.Background(), tasks)
	if gw.batchCalls != 0 {
		t.Fatalf("model batch calls = %d, want 0 for tasks routed through exec.turn", gw.batchCalls)
	}
	if got := log.all(); len(got) != 2 || !got[0] || !got[1] {
		t.Fatalf("ACP read-only arguments = %v, want [true true]", got)
	}
}

func TestTextBatchWithoutExecRouteFailsEachTask(t *testing.T) {
	tasks := summarizeTasks(2)
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: t.TempDir()},
		profile: models.AgentProfile{ID: route.DefaultAgentID},
		tasks:   make(map[string]models.Task), results: make(map[string]*models.TaskResult),
	}
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, nil, WorkerOptions{Batching: config.BatchingConfig{Enabled: true}})
	w.ProcessBatch(context.Background(), tasks)
	for _, task := range tasks {
		result := store.results[task.ID]
		if result == nil || result.Success || !strings.Contains(result.Payload, "exec.turn") {
			t.Fatalf("task %s result = %+v, want failure naming exec.turn", task.ID, result)
		}
		if state := store.tasks[task.ID].State; state != models.TaskStateFailedRequiresHuman {
			t.Fatalf("task %s state = %s, want FAILED_REQUIRES_HUMAN", task.ID, state)
		}
	}
	if gw.batchCalls != 0 {
		t.Fatalf("model batch calls = %d, want 0", gw.batchCalls)
	}
}
