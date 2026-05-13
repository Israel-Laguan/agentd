package bus

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"agentd/internal/models"
)

func TestAgentBridge_PublishAgentUpdated_NilBus(t *testing.T) {
	bridge := AgentBridge{Bus: nil}
	bridge.PublishAgentUpdated(context.Background(), models.AgentProfile{
		ID: "test",
		Name: "Test Agent",
	})
}

func TestAgentBridge_PublishAgentUpdated_WithBus(t *testing.T) {
	mb := NewInProcess()
	bridge := AgentBridge{Bus: mb}
	bridge.PublishAgentUpdated(context.Background(), models.AgentProfile{
		ID:           "test",
		Name:         "Test Agent",
		Provider:     "openai",
		Model:        "gpt-4",
		Temperature:  0.7,
		Role:         "default",
		MaxTokens:    4096,
	})
}

func TestAgentBridge_PublishAgentDeleted_NilBus(t *testing.T) {
	bridge := AgentBridge{Bus: nil}
	bridge.PublishAgentDeleted(context.Background(), "test-id")
}

func TestAgentBridge_PublishAgentDeleted_WithBus(t *testing.T) {
	mb := NewInProcess()
	bridge := AgentBridge{Bus: mb}
	bridge.PublishAgentDeleted(context.Background(), "test-id")
}

func TestAgentBridge_PublishAgentUpdated_Payload(t *testing.T) {
	mb := NewInProcess()
	bridge := AgentBridge{Bus: mb}
	bridge.PublishAgentUpdated(context.Background(), models.AgentProfile{
		ID:           "test",
		Name:         "Test Agent",
		Provider:     "openai",
		Model:        "gpt-4",
		Temperature:  0.7,
		Role:         "default",
		MaxTokens:    4096,
		SystemPrompt: sql.NullString{String: "You are helpful", Valid: true},
	})
}

func TestTaskBridge_PublishTaskAssigned_NilBus(t *testing.T) {
	bridge := TaskBridge{Bus: nil}
	bridge.PublishTaskAssigned(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
	})
}

func TestTaskBridge_PublishTaskAssigned_WithBus(t *testing.T) {
	mb := NewInProcess()
	bridge := TaskBridge{Bus: mb}
	bridge.PublishTaskAssigned(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		State:     models.TaskStatePending,
	})
}

func TestTaskBridge_PublishTaskSplit_NilBus(t *testing.T) {
	bridge := TaskBridge{Bus: nil}
	bridge.PublishTaskSplit(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "parent"}}, []models.Task{{BaseEntity: models.BaseEntity{ID: "child"}}})
}

func TestTaskBridge_PublishTaskSplit_WithBus(t *testing.T) {
	mb := NewInProcess()
	bridge := TaskBridge{Bus: mb}
	bridge.PublishTaskSplit(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "parent-1"},
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		State:     models.TaskStateRunning,
	}, []models.Task{
		{BaseEntity: models.BaseEntity{ID: "child-1"}},
		{BaseEntity: models.BaseEntity{ID: "child-2"}},
	})
}

func TestTaskBridge_PublishTaskRetried_NilBus(t *testing.T) {
	bridge := TaskBridge{Bus: nil}
	bridge.PublishTaskRetried(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
	})
}

func TestTaskBridge_PublishTaskRetried_WithBus(t *testing.T) {
	mb := NewInProcess()
	bridge := TaskBridge{Bus: mb}
	bridge.PublishTaskRetried(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "task-1"},
		ProjectID:  "proj-1",
		AgentID:    "agent-1",
		State:      models.TaskStatePending,
	})
}

func TestAgentSignalPayload_JSON(t *testing.T) {
	payload := agentSignalPayload{
		ID:           "test",
		Name:         "Test",
		Provider:     "openai",
		Model:        "gpt-4",
		Temperature:  0.7,
		Role:         "default",
		MaxTokens:    4096,
		SystemPrompt: "You are helpful",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Errorf("json.Marshal() error = %v", err)
	}
	if len(data) == 0 {
		t.Error("data should not be empty")
	}
}

func TestTaskBridge_PublishTaskSplit_EmptyChildren(t *testing.T) {
	mb := NewInProcess()
	bridge := TaskBridge{Bus: mb}
	bridge.PublishTaskSplit(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "parent-1"},
		ProjectID:  "proj-1",
	}, []models.Task{})
}

func TestTaskBridge_PublishTaskSplit_NoProjectID(t *testing.T) {
	mb := NewInProcess()
	bridge := TaskBridge{Bus: mb}
	bridge.PublishTaskSplit(context.Background(), models.Task{
		BaseEntity: models.BaseEntity{ID: "parent-1"},
		AgentID:    "agent-1",
	}, []models.Task{{BaseEntity: models.BaseEntity{ID: "child-1"}}})
}