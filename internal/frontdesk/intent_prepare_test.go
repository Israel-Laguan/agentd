package frontdesk

import (
	"strings"
	"testing"
)

type prepareIntentCase struct {
	name    string
	stash   *FileStash
	message string
	files   []InputFile
	wantErr bool
	check   func(t *testing.T, intent string, refs []FileRef)
}

func runPrepareIntentCases(t *testing.T, cases []prepareIntentCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			intent, refs, err := PrepareIntent(tt.stash, tt.message, tt.files)
			if (err != nil) != tt.wantErr {
				t.Errorf("PrepareIntent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.check != nil {
				tt.check(t, intent, refs)
			}
		})
	}
}

func TestPrepareIntent_nilStash(t *testing.T) {
	runPrepareIntentCases(t, []prepareIntentCase{
		{name: "nil stash", message: "test message", check: assertIntentUnchangedNoRefs},
		{name: "with file path and nil stash", message: "test message", files: []InputFile{{Name: "test.txt", Path: "/some/path.txt"}}, check: assertIntentUnchangedNoRefs},
		{name: "with empty content and nil stash", message: "test message", files: []InputFile{{Name: "empty.txt"}}, check: assertIntentUnchangedNoRefs},
		{name: "with no name and nil stash", message: "test message", files: []InputFile{{Path: "/some/path.txt"}}, check: assertIntentUnchangedNoRefs},
	})
}

func TestPrepareIntent_stashThreshold(t *testing.T) {
	runPrepareIntentCases(t, []prepareIntentCase{
		{
			name: "with stash threshold not met", stash: &FileStash{Dir: t.TempDir(), StashThreshold: 1000},
			message: "small message", check: assertIntentUnchanged("small message"),
		},
		{
			name: "with large message stashed", stash: &FileStash{Dir: t.TempDir(), StashThreshold: 10},
			message: "this is a very long message that exceeds the threshold", check: assertLargeMessageStashed,
		},
	})
}

func TestPrepareIntent_files(t *testing.T) {
	dir := t.TempDir()
	runPrepareIntentCases(t, []prepareIntentCase{
		{
			name: "with file path only", stash: &FileStash{Dir: dir, StashThreshold: 100},
			message: "test message", files: []InputFile{{Name: "doc.txt", Path: "/existing/path.txt"}},
			check: assertFilePathOnlyRef,
		},
		{
			name: "with file content stashed", stash: &FileStash{Dir: dir, StashThreshold: 100},
			message: "test message", files: []InputFile{{Name: "content.txt", Content: "file content here"}},
			check: assertContentFileStashed,
		},
		{
			name: "with empty file name", stash: &FileStash{Dir: dir, StashThreshold: 100},
			message: "test message", files: []InputFile{{Content: "some content"}},
			check: assertEmptyNameBecomesAttachment,
		},
		{
			name: "with empty content", stash: &FileStash{Dir: dir, StashThreshold: 100},
			message: "test message", files: []InputFile{{Name: "empty.txt"}},
			check: assertIntentMessageUnchangedNoRefs,
		},
		{
			name: "with multiple files", stash: &FileStash{Dir: dir, StashThreshold: 100},
			message: "test message",
			files: []InputFile{
				{Name: "first.txt", Path: "/path/first.txt"},
				{Name: "second.txt", Content: "content second"},
			},
			check: assertMultipleFileRefs,
		},
	})
}

func assertIntentUnchangedNoRefs(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if intent != "test message" {
		t.Errorf("expected intent %q, got %q", "test message", intent)
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs, got %d", len(refs))
	}
}

func assertIntentMessageUnchangedNoRefs(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	assertIntentUnchanged("test message")(t, intent, refs)
}

func assertIntentUnchanged(want string) func(t *testing.T, intent string, refs []FileRef) {
	return func(t *testing.T, intent string, refs []FileRef) {
		t.Helper()
		if intent != want {
			t.Errorf("expected intent %q, got %q", want, intent)
		}
		if len(refs) != 0 {
			t.Errorf("expected no refs, got %d", len(refs))
		}
	}
}

func assertLargeMessageStashed(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if !strings.Contains(intent, "User message was too large and was saved as a file reference.") {
		t.Errorf("expected stashed message placeholder, got %q", intent)
	}
	if len(refs) != 1 || refs[0].Name != "user-message.txt" || refs[0].Path == "" {
		t.Errorf("unexpected refs: %+v", refs)
	}
}

func assertFilePathOnlyRef(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if !strings.Contains(intent, "test message") || !strings.Contains(intent, "[agentd file reference]") {
		t.Errorf("unexpected intent: %q", intent)
	}
	if len(refs) != 1 || refs[0].Name != "doc.txt" || refs[0].Path != "/existing/path.txt" {
		t.Errorf("unexpected refs: %+v", refs)
	}
}

func assertContentFileStashed(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if !strings.Contains(intent, "test message") || !strings.Contains(intent, "[agentd file reference]") {
		t.Errorf("unexpected intent: %q", intent)
	}
	if len(refs) != 1 || refs[0].Name != "content.txt" || refs[0].Path == "" {
		t.Errorf("unexpected refs: %+v", refs)
	}
}

func assertEmptyNameBecomesAttachment(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if !strings.Contains(intent, "test message") {
		t.Errorf("expected intent to contain test message, got %q", intent)
	}
	if len(refs) != 1 || refs[0].Name != "attachment.txt" {
		t.Errorf("unexpected refs: %+v", refs)
	}
}

func assertMultipleFileRefs(t *testing.T, intent string, refs []FileRef) {
	t.Helper()
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
		return
	}
	if refs[0].Name != "first.txt" || refs[0].Path != "/path/first.txt" {
		t.Errorf("unexpected first ref: %+v", refs[0])
	}
	if refs[1].Name != "second.txt" {
		t.Errorf("expected second.txt, got %q", refs[1].Name)
	}
}
