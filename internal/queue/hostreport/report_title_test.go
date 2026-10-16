package hostreport

import (
	"testing"

	"agentd/internal/models"
)

// B-053: with the planner's title, the S14 live report descriptions (which say
// neither "host" nor "system") are recognized as host report tasks.
func TestReportTargetAcceptsPlannerTitleWithLiveDescriptions(t *testing.T) {
	t.Parallel()
	live := []string{
		"Read os_info.txt, cpu_info.txt, memory_info.txt, and disk_info.txt and compile a human-readable report.md summarizing the OS and hardware of this machine.",
		"Create report.md summarizing all collected OS and hardware information from the previous files",
	}
	for _, desc := range live {
		if got, ok := ReportTarget(models.HostReportTaskTitle, desc); !ok || got != "report.md" {
			t.Fatalf("ReportTarget(%q, %q) = %q, %v; want report.md", models.HostReportTaskTitle, desc, got, ok)
		}
		if _, ok := ReportTarget("Build report", desc); ok {
			t.Fatalf("ReportTarget(\"Build report\", %q) matched; the scope check (2a2e0278) should need the planner title", desc)
		}
	}
}
