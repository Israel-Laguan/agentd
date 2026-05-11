package frontdesk

import (
	"testing"

	"agentd/internal/gateway"
)

func TestLastUserMessage_FindsUserMessage(t *testing.T) {
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "You are helpful"},
		{Role: "assistant", Content: "How can I help?"},
		{Role: "user", Content: "Hello"},
	}
	_ = LastUserMessage(messages)
}

func TestLastUserMessage_NoUserMessage(t *testing.T) {
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "You are helpful"},
		{Role: "assistant", Content: "How can I help?"},
	}
	_ = LastUserMessage(messages)
}

func TestLastUserMessage_TrimsWhitespace(t *testing.T) {
	messages := []gateway.PromptMessage{
		{Role: "user", Content: "  hello  \n"},
	}
	_ = LastUserMessage(messages)
}

func TestInputFile(t *testing.T) {
	file := InputFile{
		Name:    "test.txt",
		Path:    "/path/to/test.txt",
		Content: "Hello World",
	}
	if file.Name != "test.txt" {
		t.Errorf("Name = %v, want test.txt", file.Name)
	}
	if file.Path != "/path/to/test.txt" {
		t.Errorf("Path = %v, want /path/to/test.txt", file.Path)
	}
	if file.Content != "Hello World" {
		t.Errorf("Content = %v, want Hello World", file.Content)
	}
}

func TestFileRef(t *testing.T) {
	ref := FileRef{
		Name: "file.txt",
		Path: "/path/file.txt",
	}
	if ref.Name != "file.txt" {
		t.Errorf("Name = %v, want file.txt", ref.Name)
	}
	if ref.Path != "/path/file.txt" {
		t.Errorf("Path = %v, want /path/file.txt", ref.Path)
	}
}

func TestFormatFileReferenceIntent_EmptyRefs(t *testing.T) {
	_ = FormatFileReferenceIntent("test", nil)
}

func TestFormatFileReferenceIntent_WithRefs(t *testing.T) {
	refs := []FileRef{
		{Name: "a.txt", Path: "/path/a.txt"},
		{Name: "b.txt", Path: "/path/b.txt"},
	}
	_ = FormatFileReferenceIntent("test", refs)
}

func TestPrepareIntent_NilStash(t *testing.T) {
	_, _, err := PrepareIntent(nil, "test message", nil)
	if err != nil {
		t.Errorf("PrepareIntent() error = %v", err)
	}
}

func TestPrepareIntent_WithFilePath(t *testing.T) {
	file := InputFile{
		Name: "test.txt",
		Path: "/some/path.txt",
	}
	_, _, err := PrepareIntent(nil, "test message", []InputFile{file})
	if err != nil {
		t.Errorf("PrepareIntent() error = %v", err)
	}
}

func TestPrepareIntent_WithEmptyContent(t *testing.T) {
	file := InputFile{
		Name:    "empty.txt",
		Path:    "",
		Content: "",
	}
	_, _, err := PrepareIntent(nil, "test message", []InputFile{file})
	if err != nil {
		t.Errorf("PrepareIntent() error = %v", err)
	}
}

func TestPrepareIntent_WithNoName(t *testing.T) {
	file := InputFile{
		Name:    "",
		Path:    "/some/path.txt",
		Content: "",
	}
	_, _, err := PrepareIntent(nil, "test message", []InputFile{file})
	if err != nil {
		t.Errorf("PrepareIntent() error = %v", err)
	}
}