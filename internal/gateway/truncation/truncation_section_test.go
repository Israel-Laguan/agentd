package truncation

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestPartialTruncationKeepsTheMessageSection(t *testing.T) {
	tr := &AgenticTruncator{MaxMessages: 30}
	msg := spec.PromptMessage{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 500), Section: spec.SectionFileContext}
	if got := tr.truncateSingleMessage(msg, 10); got.Section != spec.SectionFileContext {
		t.Fatalf("section after truncation = %q, want %q", got.Section, spec.SectionFileContext)
	}
}
