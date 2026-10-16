package routing

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

// B-053: the planner must name the final host-fact task with the title the
// host report renderer recognizes, or the renderer never fires and the report
// is raw command output again (B-045).
func TestPlanningPromptNamesTheSystemReportTask(t *testing.T) {
	t.Parallel()
	for _, maxTasks := range []int{0, 5} {
		prompt := phasePlanningPrompt(maxTasks)
		if !strings.Contains(prompt, `"`+models.HostReportTaskTitle+`"`) {
			t.Fatalf("planning prompt (max %d) does not ask for a task titled %q:\n%s", maxTasks, models.HostReportTaskTitle, prompt)
		}
	}
}
