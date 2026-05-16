package models

import "testing"

func TestChildResolvedForParentUnblock(t *testing.T) {
	tests := []struct {
		name  string
		state TaskState
		title string
		want  bool
	}{
		{"completed", TaskStateCompleted, "worker child", true},
		{"failed hitl", TaskStateFailed, HITLSubtaskTitleApproveTool + "deploy", true},
		{"failed worker", TaskStateFailed, "worker breakdown child", false},
		{"running", TaskStateRunning, "worker child", false},
		{"ready", TaskStateReady, HITLSubtaskTitleReview + " output", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChildResolvedForParentUnblock(tt.state, tt.title); got != tt.want {
				t.Fatalf("ChildResolvedForParentUnblock(%s, %q) = %v, want %v", tt.state, tt.title, got, tt.want)
			}
		})
	}
}
