package frontdesk

import (
	"context"
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
		{
			name:         "with empty name ref",
			intent:       "test intent",
			refs:         []FileRef{{Name: "", Path: "/path/c.txt"}},
			wantContains: []string{"test intent", "[agentd file reference]", "path: /path/c.txt"},
			wantNotContains: []string{"name:"},
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
		{
			name:    "with stash threshold not met",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 1000},
			message: "small message",
			files:   nil,
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if intent != "small message" {
					t.Errorf("expected intent unchanged, got %q", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs, got %d", len(refs))
				}
			},
		},
		{
			name:    "with large message stashed",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 10},
			message: "this is a very long message that exceeds the threshold",
			files:   nil,
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if !strings.Contains(intent, "User message was too large and was saved as a file reference.") {
					t.Errorf("expected stashed message placeholder, got %q", intent)
				}
				if len(refs) != 1 {
					t.Errorf("expected 1 ref, got %d", len(refs))
				}
				if refs[0].Name != "user-message.txt" {
					t.Errorf("expected user-message.txt, got %q", refs[0].Name)
				}
				if refs[0].Path == "" {
					t.Error("expected non-empty path")
				}
			},
		},
		{
			name:    "with file path only",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 100},
			message: "test message",
			files:   []InputFile{{Name: "doc.txt", Path: "/existing/path.txt"}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if !strings.Contains(intent, "test message") {
					t.Errorf("expected intent to contain test message, got %q", intent)
				}
				if !strings.Contains(intent, "[agentd file reference]") {
					t.Errorf("expected formatted file reference in intent, got %q", intent)
				}
				if len(refs) != 1 {
					t.Errorf("expected 1 ref, got %d", len(refs))
				}
				if refs[0].Name != "doc.txt" {
					t.Errorf("expected doc.txt, got %q", refs[0].Name)
				}
				if refs[0].Path != "/existing/path.txt" {
					t.Errorf("expected /existing/path.txt, got %q", refs[0].Path)
				}
			},
		},
		{
			name:    "with file content stashed",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 100},
			message: "test message",
			files:   []InputFile{{Name: "content.txt", Path: "", Content: "file content here"}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if !strings.Contains(intent, "test message") {
					t.Errorf("expected intent to contain test message, got %q", intent)
				}
				if !strings.Contains(intent, "[agentd file reference]") {
					t.Errorf("expected formatted file reference in intent, got %q", intent)
				}
				if len(refs) != 1 {
					t.Errorf("expected 1 ref, got %d", len(refs))
				}
				if refs[0].Name != "content.txt" {
					t.Errorf("expected content.txt, got %q", refs[0].Name)
				}
				if refs[0].Path == "" {
					t.Error("expected non-empty path")
				}
			},
		},
		{
			name:    "with empty file name",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 100},
			message: "test message",
			files:   []InputFile{{Name: "", Path: "", Content: "some content"}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if !strings.Contains(intent, "test message") {
					t.Errorf("expected intent to contain test message, got %q", intent)
				}
				if len(refs) != 1 {
					t.Errorf("expected 1 ref, got %d", len(refs))
				}
				if refs[0].Name != "attachment.txt" {
					t.Errorf("expected attachment.txt, got %q", refs[0].Name)
				}
			},
		},
		{
			name:    "with empty content",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 100},
			message: "test message",
			files:   []InputFile{{Name: "empty.txt", Path: "", Content: ""}},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if intent != "test message" {
					t.Errorf("expected intent unchanged, got %q", intent)
				}
				if len(refs) != 0 {
					t.Errorf("expected no refs for empty file, got %d", len(refs))
				}
			},
		},
		{
			name:    "with multiple files",
			stash:   &FileStash{Dir: t.TempDir(), StashThreshold: 100},
			message: "test message",
			files: []InputFile{
				{Name: "first.txt", Path: "/path/first.txt"},
				{Name: "second.txt", Path: "", Content: "content second"},
			},
			wantErr: false,
			assert: func(t *testing.T, intent string, refs []FileRef) {
				if len(refs) != 2 {
					t.Errorf("expected 2 refs, got %d", len(refs))
				}
				if refs[0].Name != "first.txt" || refs[0].Path != "/path/first.txt" {
					t.Errorf("unexpected first ref: %+v", refs[0])
				}
				if refs[1].Name != "second.txt" {
					t.Errorf("expected second.txt, got %q", refs[1].Name)
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

func TestIntentWithFileContents(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (stash *FileStash, truncator gateway.Truncator, budget int, intent string, files []FileRef)
		wantErr bool
		assert  func(t *testing.T, result string)
	}{
		{
			name: "nil stash returns intent unchanged",
			setup: func(t *testing.T) (*FileStash, gateway.Truncator, int, string, []FileRef) {
				return nil, nil, 100, "original intent", []FileRef{}
			},
			wantErr: false,
			assert: func(t *testing.T, result string) {
				if result != "original intent" {
					t.Errorf("expected original intent, got %q", result)
				}
			},
		},
		{
			name: "empty files returns intent unchanged",
			setup: func(t *testing.T) (*FileStash, gateway.Truncator, int, string, []FileRef) {
				return &FileStash{Dir: t.TempDir(), StashThreshold: 100}, nil, 100, "original intent", nil
			},
			wantErr: false,
			assert: func(t *testing.T, result string) {
				if result != "original intent" {
					t.Errorf("expected original intent, got %q", result)
				}
			},
		},
		{
			name: "with nil truncator uses default",
			setup: func(t *testing.T) (*FileStash, gateway.Truncator, int, string, []FileRef) {
				dir := t.TempDir()
				stash := &FileStash{Dir: dir, StashThreshold: 100}
				content := "test file content for reading"
				path, err := stash.Write("test.txt", content)
				if err != nil {
					t.Fatal(err)
				}
				return stash, nil, 1000, "intent", []FileRef{{Name: "test.txt", Path: path}}
			},
			wantErr: false,
			assert: func(t *testing.T, result string) {
				if !strings.Contains(result, "intent") {
					t.Errorf("expected intent in result, got %q", result)
				}
				if !strings.Contains(result, "test file content for reading") {
					t.Errorf("expected file content in result, got %q", result)
				}
			},
		},
		{
			name: "with files reads and appends content",
			setup: func(t *testing.T) (*FileStash, gateway.Truncator, int, string, []FileRef) {
				dir := t.TempDir()
				stash := &FileStash{Dir: dir, StashThreshold: 100}
				content := "important content"
				path, err := stash.Write("data.txt", content)
				if err != nil {
					t.Fatal(err)
				}
				return stash, nil, 1000, "process this", []FileRef{{Name: "data.txt", Path: path}}
			},
			wantErr: false,
			assert: func(t *testing.T, result string) {
				if !strings.Contains(result, "process this") {
					t.Errorf("expected original intent, got %q", result)
				}
				if !strings.Contains(result, "important content") {
					t.Errorf("expected file content, got %q", result)
				}
				if !strings.Contains(result, "[agentd file content]") {
					t.Errorf("expected file content marker, got %q", result)
				}
			},
		},
		{
			name: "with file without name",
			setup: func(t *testing.T) (*FileStash, gateway.Truncator, int, string, []FileRef) {
				dir := t.TempDir()
				stash := &FileStash{Dir: dir, StashThreshold: 100}
				content := "content without name"
				path, err := stash.Write("unnamed.txt", content)
				if err != nil {
					t.Fatal(err)
				}
				return stash, nil, 1000, "check", []FileRef{{Name: "", Path: path}}
			},
			wantErr: false,
			assert: func(t *testing.T, result string) {
				if !strings.Contains(result, "content without name") {
					t.Errorf("expected file content, got %q", result)
				}
				if strings.Contains(result, "name:") {
					t.Errorf("expected no name line for empty name, got %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stash, truncator, budget, intent, files := tt.setup(t)
			ctx := context.Background()
			result, err := IntentWithFileContents(ctx, stash, truncator, budget, intent, files)
			if (err != nil) != tt.wantErr {
				t.Errorf("IntentWithFileContents() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.assert != nil {
				tt.assert(t, result)
			}
		})
	}
}
