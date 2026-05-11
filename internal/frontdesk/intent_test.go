package frontdesk

import (
	"strings"
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

func TestInputFile(t *testing.T) {
	tests := []struct {
		name        string
		file        InputFile
		wantName    string
		wantPath    string
		wantContent string
	}{
		{
			name:        "full file",
			file:        InputFile{Name: "test.txt", Path: "/path/to/test.txt", Content: "Hello World"},
			wantName:    "test.txt",
			wantPath:    "/path/to/test.txt",
			wantContent: "Hello World",
		},
		{
			name:        "empty file",
			file:        InputFile{},
			wantName:    "",
			wantPath:    "",
			wantContent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.file.Name != tt.wantName {
				t.Errorf("Name = %v, want %v", tt.file.Name, tt.wantName)
			}
			if tt.file.Path != tt.wantPath {
				t.Errorf("Path = %v, want %v", tt.file.Path, tt.wantPath)
			}
			if tt.file.Content != tt.wantContent {
				t.Errorf("Content = %v, want %v", tt.file.Content, tt.wantContent)
			}
		})
	}
}

func TestFileRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      FileRef
		wantName string
		wantPath string
	}{
		{
			name:     "standard ref",
			ref:      FileRef{Name: "file.txt", Path: "/path/file.txt"},
			wantName: "file.txt",
			wantPath: "/path/file.txt",
		},
		{
			name:     "empty ref",
			ref:      FileRef{},
			wantName: "",
			wantPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.ref.Name != tt.wantName {
				t.Errorf("Name = %v, want %v", tt.ref.Name, tt.wantName)
			}
			if tt.ref.Path != tt.wantPath {
				t.Errorf("Path = %v, want %v", tt.ref.Path, tt.wantPath)
			}
		})
	}
}

func TestFormatFileReferenceIntent(t *testing.T) {
	tests := []struct {
		name            string
		intent          string
		refs            []FileRef
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "empty refs",
			intent:          "test",
			refs:            nil,
			wantContains:    []string{"test"},
			wantNotContains: []string{"[agentd file reference]", "name:", "path:"},
		},
		{
			name:   "with refs",
			intent: "test intent",
			refs: []FileRef{
				{Name: "a.txt", Path: "/path/a.txt"},
				{Name: "b.txt", Path: "/path/b.txt"},
			},
			wantContains: []string{"test intent", "[agentd file reference]", "name: a.txt", "path: /path/a.txt", "name: b.txt", "path: /path/b.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatFileReferenceIntent(tt.intent, tt.refs)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatFileReferenceIntent() missing %q in output: %s", want, got)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("FormatFileReferenceIntent() unexpectedly contains %q in output: %s", notWant, got)
				}
			}
		})
	}
}

func TestPrepareIntent(t *testing.T) {
	tests := []struct {
		name    string
		stash   *FileStash
		message string
		files   []InputFile
		wantErr bool
		assert  func(t *testing.T, intent string, refs []FileRef)
	}{
		{
			name:    "nil stash",
			stash:   nil,
			message: "test message",
			files:   nil,
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if intent != "test message" {
					t.Errorf("expected intent %q, got %q", "test message", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs, got %d", len(refs))
				}
			},
		},
		{
			name:    "with file path and nil stash",
			stash:   nil,
			message: "test message",
			files:   []InputFile{{Name: "test.txt", Path: "/some/path.txt"}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				// When stash is nil, PrepareIntent returns early without processing files
				if intent != "test message" {
					t.Errorf("expected intent %q, got %q", "test message", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs with nil stash, got %d", len(refs))
				}
			},
		},
		{
			name:    "with empty content and nil stash",
			stash:   nil,
			message: "test message",
			files:   []InputFile{{Name: "empty.txt", Path: "", Content: ""}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if intent != "test message" {
					t.Errorf("expected intent %q, got %q", "test message", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs with nil stash, got %d", len(refs))
				}
			},
		},
		{
			name:    "with no name and nil stash",
			stash:   nil,
			message: "test message",
			files:   []InputFile{{Name: "", Path: "/some/path.txt", Content: ""}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if intent != "test message" {
					t.Errorf("expected intent %q, got %q", "test message", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs with nil stash, got %d", len(refs))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, refs, err := PrepareIntent(tt.stash, tt.message, tt.files)
			if (err != nil) != tt.wantErr {
				t.Errorf("PrepareIntent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.assert != nil {
				tt.assert(t, intent, refs)
			}
		})
	}
}
