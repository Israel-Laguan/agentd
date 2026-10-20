package agentic

import (
	"testing"

	"agentd/internal/gateway"
)

func TestReadResultsAreFileContextAndOtherToolsAreToolResults(t *testing.T) {
	for tool, want := range map[string]gateway.Section{
		"read":  gateway.SectionFileContext,
		"bash":  gateway.SectionToolResults,
		"write": gateway.SectionToolResults,
	} {
		if got := toolResultSection(tool); got != want {
			t.Errorf("toolResultSection(%q) = %q, want %q", tool, got, want)
		}
	}
}
