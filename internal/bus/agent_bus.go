package bus

import (
	"context"
	"encoding/json"

	"agentd/internal/models"
)

// AgentBridge publishes agent registry signals to the in-process Bus
// without touching the durable events table. These notifications are
// best-effort: the SQLite agent_profiles table is the source of truth, and
// the bus is only used to drive the SSE cockpit.
type AgentBridge struct {
	Bus Bus
}

// PublishAgentUpdated emits an agent_updated signal carrying a JSON-encoded
// AgentProfile snapshot.
func (b AgentBridge) PublishAgentUpdated(ctx context.Context, profile models.AgentProfile) {
	if b.Bus == nil {
		return
	}
	payload := agentSignalPayload{
		ID: profile.ID, Name: profile.Name, Provider: profile.Provider,
		Model: profile.Model, Temperature: profile.Temperature,
		Role: profile.Role, MaxTokens: profile.MaxTokens,
		AgenticMode: profile.AgenticMode,
	}
	if profile.SystemPrompt.Valid {
		payload.SystemPrompt = profile.SystemPrompt.String
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.Bus.Publish(ctx, Signal{Topic: GlobalTopic, Type: "agent_updated", Payload: string(encoded)})
}

// PublishAgentDeleted emits an agent_deleted signal with the removed id.
func (b AgentBridge) PublishAgentDeleted(ctx context.Context, agentID string) {
	if b.Bus == nil {
		return
	}
	encoded, err := json.Marshal(map[string]string{"id": agentID})
	if err != nil {
		return
	}
	b.Bus.Publish(ctx, Signal{Topic: GlobalTopic, Type: "agent_deleted", Payload: string(encoded)})
}

type agentSignalPayload struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Temperature  float64 `json:"temperature"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Role         string  `json:"role"`
	MaxTokens    int     `json:"max_tokens"`
	AgenticMode  bool    `json:"agentic_mode"`
}

// TaskBridge publishes manager-loop signals (assign, split, retry) to the
// in-process Bus. The durable record of these actions lives in the events
// table via existing emitters; this bridge only feeds the SSE cockpit so
// human operators see assignments in real time.
type TaskBridge struct {
	Bus Bus
}

func (b TaskBridge) PublishTaskAssigned(ctx context.Context, task models.Task) {
	b.publishTask(ctx, "task_assigned", task)
}

func (b TaskBridge) PublishTaskSplit(ctx context.Context, parent models.Task, children []models.Task) {
	if b.Bus == nil {
		return
	}
	childIDs := make([]string, 0, len(children))
	for _, c := range children {
		childIDs = append(childIDs, c.ID)
	}
	encoded, err := json.Marshal(map[string]any{
		"task_id":    parent.ID,
		"project_id": parent.ProjectID,
		"agent_id":   parent.AgentID,
		"state":      parent.State,
		"children":   childIDs,
	})
	if err != nil {
		return
	}
	payload := string(encoded)
	b.publishToTask(ctx, "task_split", parent, payload)
}

func (b TaskBridge) PublishTaskRetried(ctx context.Context, task models.Task) {
	b.publishTask(ctx, "task_retried", task)
}

func (b TaskBridge) publishTask(ctx context.Context, eventType string, task models.Task) {
	if b.Bus == nil {
		return
	}
	encoded, err := json.Marshal(map[string]any{
		"task_id":    task.ID,
		"project_id": task.ProjectID,
		"agent_id":   task.AgentID,
		"state":      task.State,
	})
	if err != nil {
		return
	}
	payload := string(encoded)
	b.publishToTask(ctx, eventType, task, payload)
}

// publishToTask fans one task signal out to the global, task and project
// topics, tagging each copy with the task and project IDs.
func (b TaskBridge) publishToTask(ctx context.Context, eventType string, task models.Task, payload string) {
	topics := []string{GlobalTopic, "task:" + task.ID}
	if task.ProjectID != "" {
		topics = append(topics, "project:"+task.ProjectID)
	}
	for _, topic := range topics {
		b.Bus.Publish(ctx, Signal{Topic: topic, Type: eventType, Payload: payload, TaskID: task.ID, ProjectID: task.ProjectID})
	}
}
