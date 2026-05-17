package frontdesk

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway"
)

func runIntentWithFileContentsCase(
	t *testing.T,
	stash *FileStash, truncator gateway.Truncator, budget int, intent string, files []FileRef,
	wantErr bool, check func(t *testing.T, result string),
) {
	t.Helper()
	result, err := IntentWithFileContents(context.Background(), stash, truncator, budget, intent, files)
	if (err != nil) != wantErr {
		t.Errorf("IntentWithFileContents() error = %v, wantErr %v", err, wantErr)
	}
	if err == nil && check != nil {
		check(t, result)
	}
}

func TestIntentWithFileContents_nilStash(t *testing.T) {
	runIntentWithFileContentsCase(t, nil, nil, 100, "original intent", nil, false,
		func(t *testing.T, result string) {
			if result != "original intent" {
				t.Errorf("expected original intent, got %q", result)
			}
		})
}

func TestIntentWithFileContents_emptyFiles(t *testing.T) {
	stash := &FileStash{Dir: t.TempDir(), StashThreshold: 100}
	runIntentWithFileContentsCase(t, stash, nil, 100, "original intent", nil, false,
		func(t *testing.T, result string) {
			if result != "original intent" {
				t.Errorf("expected original intent, got %q", result)
			}
		})
}

func TestIntentWithFileContents_defaultTruncator(t *testing.T) {
	dir := t.TempDir()
	stash := &FileStash{Dir: dir, StashThreshold: 100}
	path, err := stash.Write("test.txt", "test file content for reading")
	if err != nil {
		t.Fatal(err)
	}
	runIntentWithFileContentsCase(t, stash, nil, 1000, "intent", []FileRef{{Name: "test.txt", Path: path}}, false,
		func(t *testing.T, result string) {
			if !strings.Contains(result, "intent") || !strings.Contains(result, "test file content for reading") {
				t.Errorf("unexpected result: %q", result)
			}
		})
}

func TestIntentWithFileContents_readsAndAppends(t *testing.T) {
	dir := t.TempDir()
	stash := &FileStash{Dir: dir, StashThreshold: 100}
	path, err := stash.Write("data.txt", "important content")
	if err != nil {
		t.Fatal(err)
	}
	runIntentWithFileContentsCase(t, stash, nil, 1000, "process this", []FileRef{{Name: "data.txt", Path: path}}, false,
		func(t *testing.T, result string) {
			if !strings.Contains(result, "process this") || !strings.Contains(result, "important content") ||
				!strings.Contains(result, "[agentd file content]") {
				t.Errorf("unexpected result: %q", result)
			}
		})
}

func TestIntentWithFileContents_unnamedFile(t *testing.T) {
	dir := t.TempDir()
	stash := &FileStash{Dir: dir, StashThreshold: 100}
	path, err := stash.Write("unnamed.txt", "content without name")
	if err != nil {
		t.Fatal(err)
	}
	runIntentWithFileContentsCase(t, stash, nil, 1000, "check", []FileRef{{Path: path}}, false,
		func(t *testing.T, result string) {
			if !strings.Contains(result, "content without name") {
				t.Errorf("expected file content, got %q", result)
			}
			if strings.Contains(result, "name:") {
				t.Errorf("expected no name line for empty name, got %q", result)
			}
		})
}
