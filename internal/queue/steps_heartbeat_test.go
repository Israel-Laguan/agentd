package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"agentd/internal/models"
)

// --- Heartbeat reconciliation steps ---

func registerHeartbeatSteps(sc *godog.ScenarioContext, state *heartbeatScenario) {
	sc.Step(`^a running task with a heartbeat older than the stale threshold$`, state.runningTaskWithStaleHeartbeat)
	sc.Step(`^a running task with a recent heartbeat$`, state.runningTaskWithFreshHeartbeat)
	sc.Step(`^the OS PID for the task is not alive$`, state.pidNotAlive)
	sc.Step(`^the OS PID for the task is alive$`, state.pidAlive)
	sc.Step(`^the heartbeat reconciliation loop runs$`, state.reconcileHeartbeats)
	sc.Step(`^the task should be reset to READY$`, state.taskShouldBeReady)
	sc.Step(`^the task should remain RUNNING$`, state.taskShouldRemainRunning)
	sc.Step(`^a HEARTBEAT_RECONCILE event should be emitted$`, state.heartbeatEventEmitted)
	sc.Step(`^no HEARTBEAT_RECONCILE event should be emitted$`, state.noHeartbeatEvent)
}

type heartbeatScenario struct {
	store   *queueStore
	daemon  *Daemon
	probe   StaticPIDProbe
	sink    *queueSink
	taskPID int
}

func newHeartbeatScenario() *heartbeatScenario {
	s := &heartbeatScenario{}
	s.store = newQueueStore()
	s.sink = &queueSink{}
	s.probe = StaticPIDProbe{}
	s.taskPID = 12345
	s.daemon = NewDaemon(s.store, nil, nil, nil, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		Probe:          StaticPIDProbe{},
		StaleAfter:     2 * time.Minute,
	})
	return s
}

func (s *heartbeatScenario) rebuildDaemon() {
	s.daemon = NewDaemon(s.store, nil, nil, nil, s.sink, DaemonOptions{
		MaxWorkers:     1,
		TaskInterval:   time.Hour,
		IntakeInterval: time.Hour,
		Probe:          s.probe,
		StaleAfter:     2 * time.Minute,
	})
}

func (s *heartbeatScenario) seedRunning(heartbeat time.Time) {
	now := time.Now().UTC()
	pid := s.taskPID
	s.store.tasks = []models.Task{{
		BaseEntity:    models.BaseEntity{ID: "hb-task", CreatedAt: now, UpdatedAt: now},
		ProjectID:     "project",
		AgentID:       "default",
		Title:         "Heartbeat Test",
		State:         models.TaskStateRunning,
		Assignee:      models.TaskAssigneeSystem,
		OSProcessID:   &pid,
		LastHeartbeat: &heartbeat,
	}}
}

func (s *heartbeatScenario) runningTaskWithStaleHeartbeat(context.Context) error {
	s.seedRunning(time.Now().UTC().Add(-5 * time.Minute))
	return nil
}

func (s *heartbeatScenario) runningTaskWithFreshHeartbeat(context.Context) error {
	s.seedRunning(time.Now().UTC())
	return nil
}

func (s *heartbeatScenario) pidNotAlive(context.Context) error {
	s.probe = StaticPIDProbe{PIDs: nil}
	s.rebuildDaemon()
	return nil
}

func (s *heartbeatScenario) pidAlive(context.Context) error {
	s.probe = StaticPIDProbe{PIDs: []int{s.taskPID}}
	s.rebuildDaemon()
	return nil
}

func (s *heartbeatScenario) reconcileHeartbeats(context.Context) error {
	return s.daemon.reconcileHeartbeats(context.Background())
}

func (s *heartbeatScenario) taskShouldBeReady(context.Context) error {
	task, err := s.store.GetTask(context.Background(), "hb-task")
	if err != nil {
		return err
	}
	if task.State != models.TaskStateReady {
		return fmt.Errorf("task state = %s, want READY", task.State)
	}
	return nil
}

func (s *heartbeatScenario) taskShouldRemainRunning(context.Context) error {
	task, err := s.store.GetTask(context.Background(), "hb-task")
	if err != nil {
		return err
	}
	if task.State != models.TaskStateRunning {
		return fmt.Errorf("task state = %s, want RUNNING", task.State)
	}
	return nil
}

func (s *heartbeatScenario) heartbeatEventEmitted(context.Context) error {
	if !s.sink.containsType("HEARTBEAT_RECONCILE") {
		return fmt.Errorf("missing HEARTBEAT_RECONCILE event")
	}
	return nil
}

func (s *heartbeatScenario) noHeartbeatEvent(context.Context) error {
	if s.sink.containsType("HEARTBEAT_RECONCILE") {
		return fmt.Errorf("unexpected HEARTBEAT_RECONCILE event")
	}
	return nil
}
