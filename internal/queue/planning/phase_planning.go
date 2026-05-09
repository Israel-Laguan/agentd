package planning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"agentd/internal/models"
)

var phasePlanningTitlePattern = regexp.MustCompile(`(?i)^plan phase (\d+)$`)

func IsPhasePlanningTask(title string) bool {
	return phasePlanningTitlePattern.MatchString(strings.TrimSpace(title))
}

func NextPhaseNumber(title string) int {
	matches := phasePlanningTitlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if len(matches) != 2 {
		return 2
	}
	phase, err := strconv.Atoi(matches[1])
	if err != nil {
		return 2
	}
	return phase + 1
}

func BuildPhaseIntent(task models.Task, project models.Project, tasks []models.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan the next phase for project %q.\n\n", project.Name)
	if project.OriginalInput != "" {
		fmt.Fprintf(&b, "Original project description:\n%s\n\n", project.OriginalInput)
	}
	if task.Description != "" {
		fmt.Fprintf(&b, "Current planning task:\n%s\n\n", task.Description)
	}
	b.WriteString("Existing project tasks:\n")
	for _, existing := range tasks {
		if existing.ID == task.ID {
			continue
		}
		fmt.Fprintf(&b, "- [%s] %s", existing.State, existing.Title)
		if existing.Description != "" {
			fmt.Fprintf(&b, ": %s", existing.Description)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nIf more work remains after this phase, include a final task titled %q.", fmt.Sprintf("Plan Phase %d", NextPhaseNumber(task.Title)))
	return strings.TrimSpace(b.String())
}

func RetitlePhaseContinuationTasks(tasks []models.DraftTask, phase int) []models.DraftTask {
	if phase < 2 {
		phase = 2
	}
	retitled := append([]models.DraftTask(nil), tasks...)
	for i := range retitled {
		if IsPhasePlanningTask(retitled[i].Title) {
			retitled[i].Title = fmt.Sprintf("Plan Phase %d", phase)
		}
	}
	return retitled
}
