package sandbox

import (
	"errors"
	"testing"

	"agentd/internal/models"
)

// TestHostFactGuardAppliesOnlyToHostQuestions pins B-077 (D2a): a bare noun
// such as memory or disk names agentd's own features as often as the host, so
// the evidence rule needs host context next to it.
func TestHostFactGuardAppliesOnlyToHostQuestions(t *testing.T) {
	t.Parallel()
	const echoWrite = `echo "step done" >> PLAN_RESULTS.log`
	tests := []struct {
		name    string
		intent  string
		blocked bool
	}{
		{"memory curator feature", "curator check task: verify the memory curator merges duplicates", false},
		{"disk watchdog feature", "disk watchdog task: check that the watchdog dedups alerts", false},
		{"storage layer refactor", "refactor the storage layer of the kanban store", false},
		{"state machine", "document the task state machine", false},
		{"task processor", "update the task processor", false},
		{"host memory", "record host memory in report.md", true},
		{"server cpu", "save server CPU model to report.md", true},
		{"processor of this machine", "report the processor of this machine", true},
		{"ram and cpu of the machine", "report the machine's RAM and CPU", true},
		{"memory of this machine", "write the memory size of this machine to report.md", true},
		{"disk on the host", "list the disk usage on the host", true},
		{"system information", "collect system information: memory and disk", true},
		{"operating system alone", "record the operating system and kernel version", true},
		{"hostname alone", "save the hostname to host.txt", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateHostFactEvidence(tt.intent, echoWrite)
			if tt.blocked {
				if !errors.Is(err, models.ErrSandboxViolation) {
					t.Fatalf("want sandbox violation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want no refusal, got %v", err)
			}
		})
	}
}
