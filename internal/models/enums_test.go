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
