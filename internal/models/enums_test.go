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
	if !TaskStateBlocked.CanTransitionTo(TaskStateFailedRequiresHuman) {
		t.Fatal("BLOCKED should transition to FAILED_REQUIRES_HUMAN")
	}
	if !TaskStateCompleted.CanTransitionTo(TaskStateNeedsContext) {
		t.Fatal("COMPLETED should transition to NEEDS_CONTEXT (tiered re-gather, detected post-commit)")
	}
	if !TaskStateRunning.CanTransitionTo(TaskStateNeedsContext) {
		t.Fatal("RUNNING should transition to NEEDS_CONTEXT")
	}
	if TaskStateNeedsContext.CanTransitionTo(TaskStateCompleted) {
		t.Fatal("NEEDS_CONTEXT should not transition directly to COMPLETED")
	}
	// NEEDS_CONTEXT revival edges
	if !TaskStateNeedsContext.CanTransitionTo(TaskStateReady) {
		t.Fatal("NEEDS_CONTEXT should transition to READY (re-gather)")
	}
	if !TaskStateNeedsContext.CanTransitionTo(TaskStateInConsideration) {
		t.Fatal("NEEDS_CONTEXT should transition to IN_CONSIDERATION")
	}
	if !TaskStateNeedsContext.CanTransitionTo(TaskStateFailed) {
		t.Fatal("NEEDS_CONTEXT should transition to FAILED")
	}
	if !TaskStateNeedsContext.CanTransitionTo(TaskStateFailedRequiresHuman) {
		t.Fatal("NEEDS_CONTEXT should transition to FAILED_REQUIRES_HUMAN")
	}
	// PENDING/READY/QUEUED -> NEEDS_CONTEXT
	if !TaskStatePending.CanTransitionTo(TaskStateNeedsContext) {
		t.Fatal("PENDING should transition to NEEDS_CONTEXT")
	}
	if !TaskStateReady.CanTransitionTo(TaskStateNeedsContext) {
		t.Fatal("READY should transition to NEEDS_CONTEXT")
	}
	if !TaskStateQueued.CanTransitionTo(TaskStateNeedsContext) {
		t.Fatal("QUEUED should transition to NEEDS_CONTEXT")
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
