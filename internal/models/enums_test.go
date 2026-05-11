package models

import "testing"

func TestTaskStateValidAndTransitions(t *testing.T) {
	if !TaskStateReady.Valid() {
		t.Fatal("TaskStateReady should be valid")
	}
	if TaskState("UNKNOWN").Valid() {
		t.Fatal("UNKNOWN task state should be invalid")
	}
	if !TaskStateReady.CanTransitionTo(TaskStateQueued) {
		t.Fatal("READY should transition to QUEUED")
	}
	if TaskStateCompleted.CanTransitionTo(TaskStateRunning) {
		t.Fatal("COMPLETED should not transition to RUNNING")
	}
	if TaskState("INVALID").CanTransitionTo(TaskStateReady) {
		t.Fatal("INVALID state should not transition to anything")
	}
}

func TestTaskAssigneeValid(t *testing.T) {
	tests := []struct {
		name string
		a    TaskAssignee
		want bool
	}{
		{"SYSTEM", TaskAssigneeSystem, true},
		{"HUMAN", TaskAssigneeHuman, true},
		{"INVALID", "INVALID", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Valid(); got != tt.want {
				t.Errorf("TaskAssignee.Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectStatusValid(t *testing.T) {
	tests := []struct {
		name string
		s    ProjectStatus
		want bool
	}{
		{"ACTIVE", ProjectStatusActive, true},
		{"COMPLETED", ProjectStatusCompleted, true},
		{"ARCHIVED", ProjectStatusArchived, true},
		{"INVALID", "INVALID", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Valid(); got != tt.want {
				t.Errorf("ProjectStatus.Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaskRelationTypeValidIncludesDependsOn(t *testing.T) {
	valid := []TaskRelationType{
		TaskRelationBlocks,
		TaskRelationSpawnedBy,
		TaskRelationDependsOn,
	}
	for _, rel := range valid {
		if !rel.Valid() {
			t.Fatalf("%s should be valid", rel)
		}
	}
	if TaskRelationType("INVALID").Valid() {
		t.Fatal("INVALID relation type should be invalid")
	}
}

func TestMemoryScopeValid(t *testing.T) {
	valid := []MemoryScope{
		MemoryScopeGlobal,
		MemoryScopeProject,
		MemoryScopeTaskCuration,
		MemoryScopeUserPref,
	}
	for _, scope := range valid {
		if !scope.Valid() {
			t.Fatalf("%s should be valid", scope)
		}
	}
	if MemoryScope("LEGACY").Valid() {
		t.Fatal("LEGACY memory scope should be invalid")
	}
}

func TestNormalizeCommentAuthor(t *testing.T) {
	tests := []struct {
		input string
		want  CommentAuthor
	}{
		{"HUMAN", CommentAuthorUser},
		{"user", CommentAuthorUser},
		{"  USER  ", CommentAuthorUser},
		{"FRONTDESK", CommentAuthorFrontdesk},
		{"WORKER_AGENT", CommentAuthorWorkerAgent},
		{"SYSTEM", CommentAuthorWorkerAgent},
		{"UNKNOWN", CommentAuthor("UNKNOWN")},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeCommentAuthor(tt.input); got != tt.want {
				t.Errorf("NormalizeCommentAuthor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
