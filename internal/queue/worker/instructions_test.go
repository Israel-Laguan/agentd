package worker

import (
	"strings"
	"testing"
)

func TestParseAgentsMD_FormattedHeadings(t *testing.T) {
	content := "# Agent Instructions\n\n" + `## **Architecture**
Use ports and adapters.

## [Known Hazards](https://example.test/hazards)
Avoid shared mutable state.

` + "## `Conventions`\n" + `Keep tests focused.

## Agent *Scope*
Backend only.
`
	instructions := parseAgentsMD(content)

	if !strings.Contains(instructions.Architecture, "ports and adapters") {
		t.Fatalf("Architecture = %q", instructions.Architecture)
	}
	if !strings.Contains(instructions.KnownHazards, "shared mutable state") {
		t.Fatalf("KnownHazards = %q", instructions.KnownHazards)
	}
	if !strings.Contains(instructions.Conventions, "tests focused") {
		t.Fatalf("Conventions = %q", instructions.Conventions)
	}
	if !strings.Contains(instructions.AgentScope, "Backend only") {
		t.Fatalf("AgentScope = %q", instructions.AgentScope)
	}
}
