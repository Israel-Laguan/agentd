package worker

import (
	"context"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

// BatchKey identifies tasks that share project and agent context.
type BatchKey struct {
	ProjectID string
	AgentID   string
}

// TaskBatch is a group of tasks processed in one LLM call.
type TaskBatch struct {
	Key     BatchKey
	Project models.Project
	Profile models.AgentProfile
	Tasks   []models.Task
}

// TaskBatcher groups claimed tasks into same-context batches.
type TaskBatcher struct {
	cfg    config.BatchingConfig
	worker *Worker
}

// NewTaskBatcher returns a batcher wired to the worker for eligibility checks.
func NewTaskBatcher(cfg config.BatchingConfig, w *Worker) *TaskBatcher {
	return &TaskBatcher{cfg: cfg, worker: w}
}

// Group partitions claimed tasks into batches. When batching is disabled, each task is its own batch.
func (b *TaskBatcher) Group(ctx context.Context, claimed []models.Task) []TaskBatch {
	if b == nil || !b.cfg.Enabled || len(claimed) == 0 {
		return singletonBatches(claimed)
	}

	partitions := make(map[BatchKey][]models.Task)
	order := make([]BatchKey, 0)
	for _, task := range claimed {
		key := BatchKey{ProjectID: task.ProjectID, AgentID: task.AgentID}
		if _, ok := partitions[key]; !ok {
			order = append(order, key)
		}
		partitions[key] = append(partitions[key], task)
	}

	var out []TaskBatch
	for _, key := range order {
		out = append(out, b.groupPartition(ctx, key, partitions[key])...)
	}
	return out
}

func (b *TaskBatcher) groupPartition(ctx context.Context, key BatchKey, tasks []models.Task) []TaskBatch {
	var batches []TaskBatch
	var pending []models.Task

	flush := func() {
		if len(pending) == 0 {
			return
		}
		batches = append(batches, b.buildBatch(ctx, key, pending))
		pending = nil
	}

	for _, task := range tasks {
		if !b.worker.isTaskBatchable(ctx, task) {
			flush()
			batches = append(batches, b.singletonBatch(ctx, key, task))
			continue
		}
		if len(pending) >= b.cfg.MaxBatchSize || !b.independentWith(pending, task) {
			flush()
		}
		pending = append(pending, task)
	}
	flush()
	return batches
}

func (b *TaskBatcher) buildBatch(ctx context.Context, key BatchKey, tasks []models.Task) TaskBatch {
	project, profile, _ := b.worker.loadContext(ctx, tasks[0])
	if project == nil {
		project = &models.Project{BaseEntity: models.BaseEntity{ID: key.ProjectID}}
	}
	if profile == nil {
		profile = &models.AgentProfile{ID: key.AgentID}
	}
	return TaskBatch{
		Key:     key,
		Project: *project,
		Profile: *profile,
		Tasks:   append([]models.Task(nil), tasks...),
	}
}

func (b *TaskBatcher) singletonBatch(ctx context.Context, key BatchKey, task models.Task) TaskBatch {
	batch := b.buildBatch(ctx, key, []models.Task{task})
	return batch
}

func singletonBatches(tasks []models.Task) []TaskBatch {
	out := make([]TaskBatch, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, TaskBatch{
			Key:   BatchKey{ProjectID: task.ProjectID, AgentID: task.AgentID},
			Tasks: []models.Task{task},
		})
	}
	return out
}

func (b *TaskBatcher) independentWith(batch []models.Task, task models.Task) bool {
	if len(batch) == 0 {
		return true
	}
	ids := make(map[string]struct{}, len(batch)+1)
	for _, t := range batch {
		ids[t.ID] = struct{}{}
	}
	for _, dep := range task.DependsOn {
		if _, ok := ids[dep]; ok {
			return false
		}
	}
	for _, t := range batch {
		for _, dep := range t.DependsOn {
			if dep == task.ID {
				return false
			}
		}
	}
	return true
}

func (w *Worker) isTaskBatchable(ctx context.Context, task models.Task) bool {
	if planning.IsPhasePlanningTask(task.Title) {
		return false
	}
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		return false
	}
	if profile.RequireReview {
		return false
	}
	if w.wouldCapabilityRoute(task, *profile) {
		return false
	}
	if profile.AgenticMode {
		return !w.requiresAgenticTools(ctx, task, *project, *profile)
	}
	return true
}

func (w *Worker) wouldCapabilityRoute(task models.Task, profile models.AgentProfile) bool {
	if w.capabilityRouter == nil {
		return false
	}
	_, ok := w.capabilityRouter.Route(task, profile)
	return ok
}

func (w *Worker) requiresAgenticTools(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
) bool {
	toolExec := w.newAgenticTaskToolExecutor(project, task)
	tools, index := w.agenticTools(ctx, toolExec)
	tools, _ = w.filterAgenticTools(tools, index, task, profile)
	return len(tools) > 0
}

// GroupClaimed is the dispatch entry point for batch grouping.
func (w *Worker) GroupClaimed(ctx context.Context, tasks []models.Task) []TaskBatch {
	if w.batcher == nil {
		return singletonBatches(tasks)
	}
	return w.batcher.Group(ctx, tasks)
}
