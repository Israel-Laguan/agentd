package frontdesk

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway"
)

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
