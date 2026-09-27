//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TaskState mirrors internal/models.TaskState's real enum values.
type TaskState string

const (
	TaskStatePending             TaskState = "PENDING"
	TaskStateReady               TaskState = "READY"
	TaskStateQueued              TaskState = "QUEUED"
	TaskStateRunning             TaskState = "RUNNING"
	TaskStateBlocked             TaskState = "BLOCKED"
	TaskStateCompleted           TaskState = "COMPLETED"
	TaskStateFailed              TaskState = "FAILED"
	TaskStateFailedRequiresHuman TaskState = "FAILED_REQUIRES_HUMAN"
	TaskStateNeedsContext        TaskState = "NEEDS_CONTEXT"
	TaskStateInConsideration     TaskState = "IN_CONSIDERATION"
)

// Task represents a task object from the API (internal/models.Task). There
// is no per-task GET endpoint; tasks are only observable via the
// project-scoped list (GET /api/v1/projects/{id}/tasks) or as the payload of
// a mutating call (materialize, workspace/ready, patch, assign, ...).
type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       TaskState `json:"state"`
	DependsOn   []string  `json:"depends_on"`
}

// TaskPoller polls a project's task list until one task reaches a desired
// state, or until every task in the project reaches a terminal state.
type TaskPoller struct {
	client    *APIClient
	projectID string
}

// NewTaskPoller creates a task poller for the given project (by UUID, as
// returned by MaterializePlan — not the human-readable project name).
func NewTaskPoller(client *APIClient, projectID string) *TaskPoller {
	return &TaskPoller{
		client:    client,
		projectID: projectID,
	}
}

// WaitForTaskState polls until the task with the given ID reaches the
// desired state. Returns an error immediately if the task instead reaches
// FAILED or FAILED_REQUIRES_HUMAN.
func (p *TaskPoller) WaitForTaskState(ctx context.Context, taskID string, desired TaskState, timeout time.Duration) (*Task, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("task poller timeout waiting for task %s to reach %s", taskID, desired)
			}
			tasks, err := p.listTasks(ctx)
			if err != nil {
				continue
			}
			for _, task := range tasks {
				if task.ID != taskID {
					continue
				}
				if task.State == desired {
					return &task, nil
				}
				if task.State == TaskStateFailed || task.State == TaskStateFailedRequiresHuman {
					return &task, fmt.Errorf("task %s reached %s", taskID, task.State)
				}
			}
		}
	}
}

// WaitForAllComplete polls until every task in the project is COMPLETED.
// Returns an error if any task reaches FAILED or FAILED_REQUIRES_HUMAN, or
// on timeout, along with the last observed task list for diagnostics.
func (p *TaskPoller) WaitForAllComplete(ctx context.Context, timeout time.Duration) ([]Task, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	var last []Task
	for {
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
			tasks, err := p.listTasks(ctx)
			if err != nil {
				continue
			}
			last = tasks
			if len(tasks) == 0 {
				continue
			}
			allDone := true
			for _, task := range tasks {
				if task.State == TaskStateFailed || task.State == TaskStateFailedRequiresHuman {
					return tasks, fmt.Errorf("task %s (%s) reached %s", task.ID, task.Title, task.State)
				}
				if task.State != TaskStateCompleted {
					allDone = false
				}
			}
			if allDone {
				return tasks, nil
			}
			if time.Now().After(deadline) {
				return tasks, fmt.Errorf("task poller timeout waiting for all tasks to complete")
			}
		}
	}
}

func (p *TaskPoller) listTasks(ctx context.Context) ([]Task, error) {
	resp, err := p.client.ListTasks(ctx, p.projectID, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tasks returned %d", resp.StatusCode)
	}

	var envelope struct {
		Data []Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}
