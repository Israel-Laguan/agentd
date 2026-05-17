package frontdesk

import (
	"strings"
	"testing"
)

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
