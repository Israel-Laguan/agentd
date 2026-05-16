package worker

import (
	"strings"
	"testing"
)

func TestBuildClarificationDetail_QuestionOnly(t *testing.T) {
	t.Parallel()
	msg := ClarificationMessage{
		Question: "Which database should I target?",
	}
	detail := buildClarificationDetail(msg)
	if !strings.Contains(detail, "Which database") {
		t.Fatalf("detail should contain the question, got %q", detail)
	}
}

func TestBuildClarificationDetail_WithOptions(t *testing.T) {
	t.Parallel()
	msg := ClarificationMessage{
		Question: "Which environment?",
		Options:  []string{"staging", "production"},
	}
	detail := buildClarificationDetail(msg)
	if !strings.Contains(detail, "Options:") {
		t.Fatalf("detail should contain Options header, got %q", detail)
	}
	if !strings.Contains(detail, "1. staging") {
		t.Fatalf("detail should list first option, got %q", detail)
	}
	if !strings.Contains(detail, "2. production") {
		t.Fatalf("detail should list second option, got %q", detail)
	}
}

func TestBuildClarificationDetail_WithContext(t *testing.T) {
	t.Parallel()
	msg := ClarificationMessage{
		Question:       "Which branch?",
		ContextSummary: "The task involves deploying a hotfix.",
	}
	detail := buildClarificationDetail(msg)
	if !strings.Contains(detail, "Context:") {
		t.Fatalf("detail should contain Context header, got %q", detail)
	}
	if !strings.Contains(detail, "deploying a hotfix") {
		t.Fatalf("detail should contain context summary, got %q", detail)
	}
}

func TestBuildClarificationDetail_Full(t *testing.T) {
	t.Parallel()
	msg := ClarificationMessage{
		Question:       "Which target?",
		Options:        []string{"A", "B"},
		ContextSummary: "Context here",
	}
	detail := buildClarificationDetail(msg)
	if !strings.Contains(detail, "Which target?") {
		t.Fatalf("missing question in detail: %q", detail)
	}
	if !strings.Contains(detail, "1. A") {
		t.Fatalf("missing option A in detail: %q", detail)
	}
	if !strings.Contains(detail, "Context here") {
		t.Fatalf("missing context in detail: %q", detail)
	}
}

func TestClarificationMessage_Fields(t *testing.T) {
	t.Parallel()
	msg := ClarificationMessage{
		Question:       "test question",
		Options:        []string{"opt1", "opt2"},
		ContextSummary: "test context",
		TaskID:         "task-abc",
	}
	if msg.Question != "test question" {
		t.Fatalf("Question = %q, want %q", msg.Question, "test question")
	}
	if len(msg.Options) != 2 {
		t.Fatalf("Options len = %d, want 2", len(msg.Options))
	}
	if msg.TaskID != "task-abc" {
		t.Fatalf("TaskID = %q, want %q", msg.TaskID, "task-abc")
	}
}

func TestClarificationResponse_Fields(t *testing.T) {
	t.Parallel()
	resp := ClarificationResponse{
		Answer:   "staging",
		Selected: "opt1",
	}
	if resp.Answer != "staging" {
		t.Fatalf("Answer = %q, want %q", resp.Answer, "staging")
	}
	if resp.Selected != "opt1" {
		t.Fatalf("Selected = %q, want %q", resp.Selected, "opt1")
	}
}
