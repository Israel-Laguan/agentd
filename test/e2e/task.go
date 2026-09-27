//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TaskState represents the state of a task.
type TaskState string

const (
	TaskStateReady      TaskState = "READY"
	TaskStateRunning    TaskState = "RUNNING"
	TaskStateCompleted  TaskState = "COMPLETED"
	TaskStateHuman      TaskState = "HUMAN"
	TaskStateError      TaskState = "ERROR"
	TaskStateCancelled  TaskState = "CANCELLED"
)

// Task represents a task object from the API.
type Task struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	State    TaskState `json:"state"`
	Output   string    `json:"output,omitempty"`
	Error    string    `json:"error,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// TaskPoller polls a task until a desired state is reached or a timeout occurs.
type TaskPoller struct {
	client      *APIClient
	projectName string
	taskID      string
}

// NewTaskPoller creates a task poller for the given project and task.
func NewTaskPoller(client *APIClient, projectName, taskID string) *TaskPoller {
	return &TaskPoller{
		client:      client,
		projectName: projectName,
		taskID:      taskID,
	}
}

// WaitForState polls the task until it reaches the desired state.
// Returns the final task state or an error if timeout is exceeded.
func (p *TaskPoller) WaitForState(ctx context.Context, desiredState TaskState, timeout time.Duration) (*Task, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(deadline)):
			return nil, fmt.Errorf("task poller timeout waiting for state %s", desiredState)
		case <-ticker.C:
			task, err := p.getTask(ctx)
			if err != nil {
				continue
			}

			if task.State == desiredState {
				return task, nil
			}

			// Error states fail immediately.
			if task.State == TaskStateError {
				return task, fmt.Errorf("task reached ERROR state: %s", task.Error)
			}
		}
	}
}

// CurrentState returns the current state of the task.
func (p *TaskPoller) CurrentState(ctx context.Context) (*Task, error) {
	return p.getTask(ctx)
}

func (p *TaskPoller) getTask(ctx context.Context) (*Task, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/tasks/%s", p.projectName, p.taskID)
	resp, err := p.client.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get task returned %d", resp.StatusCode)
	}

	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, err
	}

	return &task, nil
}
