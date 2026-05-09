package models

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestBuildExecutionPayload(t *testing.T) {
	task := Task{
		BaseEntity:  BaseEntity{ID: "task-1"},
		ProjectID:   "project-1",
		Description: "implement the queue",
	}
	project := Project{
		BaseEntity:    BaseEntity{ID: "project-1"},
		OriginalInput: "build a reliable worker system",
	}
	history := []Event{
		eventForTask("task-1", "LOG", "first failure log"),
		eventForTask("task-2", "LOG", "other task log"),
		eventForTask("task-1", "COMMENT", "not an attempt"),
		eventForTask("task-1", "FAILURE", "second failure summary"),
		{ProjectID: "project-1", Type: "LOG", Payload: "project log"},
	}

	payload := BuildExecutionPayload(task, project, history)

	if payload.TaskID != task.ID || payload.ProjectID != task.ProjectID {
		t.Fatalf("unexpected payload identity: %#v", payload)
	}
	if payload.Why != project.OriginalInput || payload.What != task.Description {
		t.Fatalf("unexpected payload context: %#v", payload)
	}
	wantAttempts := []string{"first failure log", "second failure summary"}
	if !reflect.DeepEqual(payload.PreviousAttempts, wantAttempts) {
		t.Fatalf("PreviousAttempts = %#v, want %#v", payload.PreviousAttempts, wantAttempts)
	}
}

func eventForTask(taskID string, eventType EventType, payload string) Event {
	return Event{
		ProjectID: "project-1",
		TaskID:    sql.NullString{String: taskID, Valid: true},
		Type:      eventType,
		Payload:   payload,
	}
}
