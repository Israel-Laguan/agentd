package main

import (
	"strings"
	"testing"
)

func TestStatusCommand_PrintsCounts(t *testing.T) {
	home := initHome(t)
	runCLI(t, home, "project", "create", "--name", "status-cli", "--description", "seed tasks for status")

	output := runCLI(t, home, "status")
	for _, expect := range []string{
		"STATE             COUNT",
		"READY",
		"queue_length",
		"active_threads",
	} {
		if !strings.Contains(output, expect) {
			t.Errorf("output missing %q\nfull output:\n%s", expect, output)
		}
	}
}
