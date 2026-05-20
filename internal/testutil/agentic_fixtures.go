package testutil

import "strings"

// AgenticTestTaskDescription returns a task description long and constrained enough
// to skip pre-task elicitation in agentic integration tests.
func AgenticTestTaskDescription() string {
	return strings.Repeat(
		"Execute the agentic test scenario with explicit scope and acceptance criteria. ", 10) +
		"\n- must complete without human clarification\n" +
		"- must use internal/testutil/agentic_fixtures.go as a reference path\n" +
		"Acceptance: returns the expected committed output."
}
