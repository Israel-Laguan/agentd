package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"agentd/internal/models"
)

// --- Resilience steps (outage handoff, disk watchdog) ---

func registerResilienceSteps(sc *godog.ScenarioContext, state *resilienceScenario) {
	// Outage handoff
	sc.Step(`^the circuit breaker has been open for longer than the handoff threshold$`, state.breakerOpenBeyondThreshold)
	sc.Step(`^the daemon checks for outage handoff$`, state.checkOutageHandoff)
	sc.Step(`^a HUMAN task titled "([^"]*)" should exist under the _system project$`, state.humanTaskExists)
	sc.Step(`^an LLM_OUTAGE_HANDOFF event should be emitted$`, state.outageHandoffEmitted)
	sc.Step(`^an outage HUMAN task already exists under the _system project$`, state.outageTaskAlreadyExists)
	sc.Step(`^the daemon checks for outage handoff again$`, state.checkOutageHandoff)
	sc.Step(`^no additional outage task should be created$`, state.noAdditionalOutageTask)
	sc.Step(`^the circuit breaker has been open for less than the handoff threshold$`, state.breakerOpenBelowThreshold)
	sc.Step(`^no outage task should be created$`, state.noOutageTask)

	// Disk watchdog
	sc.Step(`^the disk free percentage is below the configured threshold$`, state.diskBelowThreshold)
	sc.Step(`^the disk watchdog checks free space$`, state.checkDiskSpace)
	sc.Step(`^a DISK_SPACE_CRITICAL event should be emitted$`, state.diskCriticalEmitted)
	sc.Step(`^a disk alert HUMAN task already exists under the _system project$`, state.diskAlertAlreadyExists)
	sc.Step(`^the disk watchdog checks free space again$`, state.checkDiskSpace)
	sc.Step(`^no additional disk alert task should be created$`, state.noAdditionalDiskAlert)
	sc.Step(`^the disk free percentage is above the configured threshold$`, state.diskAboveThreshold)
	sc.Step(`^no disk alert task should be created$`, state.noDiskAlert)
}

type resilienceScenario struct {
	store          *resilienceStore
	breaker        *CircuitBreaker
	daemon         *Daemon
	sink           *queueSink
	systemTasksLen int
}

type resilienceStore struct {
	queueStore
	systemTasks []models.Task
}

func (s *resilienceStore) EnsureSystemProject(_ context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}

func (s *resilienceStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	for i := range s.systemTasks {
		if s.systemTasks[i].Title == draft.Title {
			return &s.systemTasks[i], false, nil
		}
	}
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: fmt.Sprintf("sys-task-%d", len(s.systemTasks))},
		ProjectID:   projectID,
		Title:       draft.Title,
		Description: draft.Description,
		Assignee:    draft.Assignee,
	}
	s.systemTasks = append(s.systemTasks, task)
	return &task, true, nil
}

func newResilienceScenario() *resilienceScenario {
	s := &resilienceScenario{}
	s.store = &resilienceStore{queueStore: *newQueueStore()}
	s.breaker = NewCircuitBreaker()
	s.sink = &queueSink{}
	s.daemon = NewDaemon(s.store, nil, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		HandoffAfter:   2 * time.Minute,
	})
	return s
}

func (s *resilienceScenario) breakerOpenBeyondThreshold(context.Context) error {
	now := time.Now().UTC()
	s.breaker.ArmForResilienceTest(now, now.Add(-3*time.Minute), 5, models.ErrLLMUnreachable)
	return nil
}

func (s *resilienceScenario) checkOutageHandoff(context.Context) error {
	return s.daemon.checkOutageHandoff(context.Background())
}

func (s *resilienceScenario) humanTaskExists(_ context.Context, title string) error {
	for _, t := range s.store.systemTasks {
		if t.Title == title && t.Assignee == models.TaskAssigneeHuman {
			return nil
		}
	}
	return fmt.Errorf("no HUMAN task with title %q", title)
}

func (s *resilienceScenario) outageHandoffEmitted(context.Context) error {
	if !s.sink.containsType("LLM_OUTAGE_HANDOFF") {
		return fmt.Errorf("missing LLM_OUTAGE_HANDOFF event")
	}
	return nil
}

func (s *resilienceScenario) outageTaskAlreadyExists(context.Context) error {
	err := s.daemon.checkOutageHandoff(context.Background())
	if err != nil {
		return err
	}
	s.systemTasksLen = len(s.store.systemTasks)
	return nil
}

func (s *resilienceScenario) noAdditionalOutageTask(context.Context) error {
	if len(s.store.systemTasks) != s.systemTasksLen {
		return fmt.Errorf("tasks count changed from %d to %d", s.systemTasksLen, len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) breakerOpenBelowThreshold(context.Context) error {
	now := time.Now().UTC()
	s.breaker.ArmForResilienceTest(now, now.Add(-30*time.Second), 0, nil)
	return nil
}

func (s *resilienceScenario) noOutageTask(context.Context) error {
	if len(s.store.systemTasks) != 0 {
		return fmt.Errorf("unexpected %d tasks created", len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) diskBelowThreshold(context.Context) error {
	s.daemon.diskFreeThreshold = 10.0
	s.daemon.diskCheckPath = "/tmp"
	s.daemon.diskStat = func(_ string) (float64, error) { return 5.0, nil }
	return nil
}

func (s *resilienceScenario) checkDiskSpace(context.Context) error {
	return s.daemon.checkDiskSpace(context.Background())
}

func (s *resilienceScenario) diskCriticalEmitted(context.Context) error {
	if !s.sink.containsType("DISK_SPACE_CRITICAL") {
		return fmt.Errorf("missing DISK_SPACE_CRITICAL event")
	}
	return nil
}

func (s *resilienceScenario) diskAlertAlreadyExists(context.Context) error {
	err := s.daemon.checkDiskSpace(context.Background())
	if err != nil {
		return err
	}
	s.systemTasksLen = len(s.store.systemTasks)
	return nil
}

func (s *resilienceScenario) noAdditionalDiskAlert(context.Context) error {
	if len(s.store.systemTasks) != s.systemTasksLen {
		return fmt.Errorf("tasks count changed from %d to %d", s.systemTasksLen, len(s.store.systemTasks))
	}
	return nil
}

func (s *resilienceScenario) diskAboveThreshold(context.Context) error {
	s.daemon.diskFreeThreshold = 10.0
	s.daemon.diskCheckPath = "/tmp"
	s.daemon.diskStat = func(_ string) (float64, error) { return 50.0, nil }
	return nil
}

func (s *resilienceScenario) noDiskAlert(context.Context) error {
	if len(s.store.systemTasks) != 0 {
		return fmt.Errorf("unexpected %d tasks created", len(s.store.systemTasks))
	}
	return nil
}
