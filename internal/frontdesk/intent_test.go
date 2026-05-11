package frontdesk

import (
	"testing"

	"agentd/internal/gateway"
)

func TestLastUserMessage(t *testing.T) {
	tests := []struct {
		name     string
		messages []gateway.PromptMessage
		want     string
	}{
		{
			name: "single user message",
			messages: []gateway.PromptMessage{
				{Role: "user", Content: "hello"},
			},
			want: "hello",
		},
		{
			name: "user message with padding",
			messages: []gateway.PromptMessage{
				{Role: "user", Content: "  hello  "},
			},
			want: "hello",
		},
		{
			name: "multiple messages, last user wins",
			messages: []gateway.PromptMessage{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "last"},
			},
			want: "last",
		},
		{
			name: "no user message",
			messages: []gateway.PromptMessage{
				{Role: "system", Content: "init"},
				{Role: "assistant", Content: "ready"},
			},
			want: "",
		},
		{
			name:     "empty messages",
			messages: []gateway.PromptMessage{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LastUserMessage(tt.messages); got != tt.want {
				t.Errorf("LastUserMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatFileReferenceIntent(t *testing.T) {
	intent := "test intent"
	refs := []FileRef{
		{Name: "file1.txt", Path: "/path/to/file1.txt"},
		{Name: "file2.txt", Path: "/path/to/file2.txt"},
	}

	got := FormatFileReferenceIntent(intent, refs)
	if !contains(got, "test intent") {
		t.Errorf("expected intent in output")
	}
	if !contains(got, "[agentd file reference]") {
		t.Errorf("expected file reference header")
	}
	if !contains(got, "name: file1.txt") || !contains(got, "path: /path/to/file1.txt") {
		t.Errorf("expected file1 details")
	}
	if !contains(got, "name: file2.txt") || !contains(got, "path: /path/to/file2.txt") {
		t.Errorf("expected file2 details")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
