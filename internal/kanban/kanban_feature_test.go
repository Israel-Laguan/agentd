package kanban

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"

	"github.com/cucumber/godog"
)

func TestKanbanFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeKanbanScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
			Strict:   true,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

type kanbanScenario struct {
	store         *Store
	project       *models.Project
	plan          models.DraftPlan
	tasks         map[string]models.Task
	claimed       []models.Task
	recovered     []models.Task
	alivePIDs     []int
	lastErr       error
	lastTaskTitle string
}

//revive:disable:function-length
//nolint:funlen
func initializeKanbanScenario(sc *godog.ScenarioContext) {
	state := &kanbanScenario{}

	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		dbName := strings.NewReplacer(" ", "_", "/", "_").Replace(scenario.Name)
		db, err := Open("file:godog-" + dbName + "?mode=memory&cache=shared")
		if err != nil {
			return ctx, fmt.Errorf("open test store: %w", err)
		}
		state.store = NewStore(db)
		state.project = nil
		state.plan = models.DraftPlan{}
		state.tasks = make(map[string]models.Task)
		state.claimed = nil
		state.recovered = nil
		state.alivePIDs = nil
		state.lastErr = nil
		state.lastTaskTitle = ""
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if state.store != nil {
			if err := state.store.Close(); err != nil {
				return ctx, fmt.Errorf("close test store: %w", err)
			}
		}
		return ctx, nil
	})

	sc.Step(`^a draft plan with tasks (.+)$`, state.aDraftPlanWithTasks)
	sc.Step(`^I have a valid DraftPlan with (\d+) tasks: (.+)$`, state.iHaveValidDraftPlanWithTasks)
	sc.Step(`^I have a DraftPlan where Task A depends on Task B$`, state.iHaveCircularDraftPlan)
	sc.Step(`^([A-Z0-9]+(?: and [A-Z0-9]+)*) depend on ([A-Z0-9]+)$`, state.tasksDependOn)
	sc.Step(`^"([^"]+)" depends on "([^"]+)"$`, state.taskDependsOn)
	sc.Step(`^([A-Z0-9]+) depends on (.+)$`, state.taskDependsOn)
	sc.Step(`^Task ([A-Z0-9]+) depends on Task ([A-Z0-9]+)$`, state.taskDependsOn)
	sc.Step(`^the plan is materialized$`, state.thePlanIsMaterialized)
	sc.Step(`^I call MaterializePlan(?: with this plan)?$`, state.iCallMaterializePlan)
	sc.Step(`^a new project should be created in the projects table$`, state.oneProjectShouldBeCreated)
	sc.Step(`^(\d+) tasks should be created in the tasks table$`, state.tasksShouldBeCreated)
	sc.Step(`^the "([^"]+)" task should have the state ([A-Z_]+)$`, state.namedTaskShouldHaveState)
	sc.Step(`^the "([^"]+)" and "([^"]+)" tasks should have the state ([A-Z_]+)$`, state.twoNamedTasksShouldHaveState)
	sc.Step(`^the task_relations table should have (\d+) entries mapping the dependencies$`, state.taskRelationsShouldHaveEntries)
	sc.Step(`^a dependency edge from ([A-Z0-9]+) to ([A-Z0-9]+) is tested for cycles$`, state.dependencyEdgeTestedForCycles)
	sc.Step(`^the operation should return a CircularDependencyError$`, state.operationShouldReturnCircularDependencyError)
	sc.Step(`^no records should be written to the projects or tasks tables$`, state.noProjectOrTaskRecordsShouldBeWritten)
	sc.Step(`^tasks (.+) should be ready$`, state.tasksShouldBeReady)
	sc.Step(`^tasks (.+) should be pending$`, state.tasksShouldBePending)
	sc.Step(`^tasks (.+) should be blocked$`, state.tasksShouldBeBlocked)
	sc.Step(`^task (.+) is blocked with subtasks (.+)$`, state.taskIsBlockedWithSubtasks)
	sc.Step(`^task (.+) is completed$`, state.taskIsCompleted)
	sc.Step(`^tasks (.+) should be claimable$`, state.tasksShouldBeClaimable)
	sc.Step(`^the database has 1 task in READY state$`, state.databaseHasOneReadyTask)
	sc.Step(`^1 task in PENDING state waiting on a dependency$`, state.databaseHasOnePendingTask)
	sc.Step(`^1 task in COMPLETED state$`, state.databaseHasOneCompletedTask)
	sc.Step(`^the Queue calls ClaimNextReadyTasks with a limit of (\d+)$`, state.queueCallsClaimNextReadyTasks)
	sc.Step(`^only the 1 QUEUED task should be returned$`, state.onlyOneQueuedTaskShouldBeReturned)
	sc.Step(`^its state in the database should now be QUEUED$`, state.claimedTaskStateShouldBeQueued)
	sc.Step(`^Task ([A-Z0-9]+) is ([A-Z_]+)$`, state.taskIsInState)
	sc.Step(`^Task ([A-Z0-9]+) is ([A-Z_]+) and depends on Task ([A-Z0-9]+)$`, state.taskIsInStateAndDependsOnTask)
	sc.Step(`^the Worker calls UpdateTaskResult for Task ([A-Z0-9]+) with Success=true$`, state.workerCallsUpdateTaskResultSuccess)
	sc.Step(`^Task ([A-Z0-9]+) should move to ([A-Z_]+)$`, state.taskShouldMoveTo)
	sc.Step(`^Task ([A-Z0-9]+) should automatically move from ([A-Z_]+) to ([A-Z_]+)$`, state.taskShouldMoveFromTo)
	sc.Step(`^Task "([^"]+)" is in ([A-Z_]+) state$`, state.taskIsInState)
	sc.Step(`^a Human adds a comment to Task "([^"]+)"$`, state.humanAddsACommentToTask)
	sc.Step(`^the state should change to ([A-Z_]+)$`, state.lastTaskStateShouldChangeTo)
	sc.Step(`^if the Agent subsequently tries to call UpdateTaskResult for Task "([^"]+)"$`, state.agentSubsequentlyTriesUpdateTaskResult)
	sc.Step(`^the database update should fail with a StateConflictError$`, state.databaseUpdateShouldFailWithStateConflict)
	sc.Step(`^the task state should remain ([A-Z_]+)$`, state.lastTaskStateShouldRemain)
	sc.Step(`^a task in the database is marked RUNNING with os_process_id (\d+)$`, state.taskIsRunningWithPID)
	sc.Step(`^the operating system reports that PID (\d+) does not exist$`, state.pidDoesNotExist)
	sc.Step(`^the daemon runs ReconcileGhostTasks on startup$`, state.daemonRunsReconcileGhostTasks)
	sc.Step(`^the task state should be reset to READY$`, state.taskStateShouldBeResetToReady)
	sc.Step(`^a RECOVERY event should be logged for that task$`, state.recoveryEventShouldBeLogged)
	sc.Step(`^a project exists with (\d+) tasks and (\d+) events$`, state.projectExistsWithTasksAndEvents)
	sc.Step(`^I delete the project from the projects table$`, state.deleteProjectFromProjectsTable)
	sc.Step(`^all associated tasks should be deleted from the tasks table$`, state.tasksTableShouldBeEmpty)
	sc.Step(`^all associated events should be deleted from the events table$`, state.eventsTableShouldBeEmpty)
	sc.Step(`^all associated relations should be deleted from the task_relations table$`, state.relationsTableShouldBeEmpty)
}

//revive:enable:function-length
