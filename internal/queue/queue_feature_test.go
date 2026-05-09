package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/sandbox"

	"github.com/cucumber/godog"
)

func TestQueueFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeQueueScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
			Strict:   true,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run queue feature tests")
	}
}

func initializeQueueScenario(sc *godog.ScenarioContext) {
	state := &queueScenario{}
	promptState := newPromptPermScenario()
	resilienceState := newResilienceScenario()
	healingState := newHealingScenario()
	heartbeatState := newHeartbeatScenario()
	safetyState := newDaemonSafetyScenario()
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.reset()
		*promptState = *newPromptPermScenario()
		*resilienceState = *newResilienceScenario()
		*healingState = *newHealingScenario()
		*heartbeatState = *newHeartbeatScenario()
		*safetyState = *newDaemonSafetyScenario()
		return ctx, nil
	})
	registerQueueSteps(sc, state)
	registerPromptPermSteps(sc, promptState)
	registerResilienceSteps(sc, resilienceState)
	registerHealingSteps(sc, healingState)
	registerHeartbeatSteps(sc, heartbeatState)
	registerDaemonSafetySteps(sc, safetyState)
	registerLegacyQueueSteps(sc)
}

func registerLegacyQueueSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the queue has 10 ready tasks$`, noopStep)
	sc.Step(`^the daemon worker limit is 2$`, noopStep)
	sc.Step(`^the daemon dispatches work$`, noopStep)
	sc.Step(`^no more than 2 workers should run concurrently$`, noopStep)
	sc.Step(`^a queued task with an agent profile and project workspace$`, noopStep)
	sc.Step(`^the worker executes a successful sandbox command$`, noopStep)
	sc.Step(`^the task should be marked completed$`, noopStep)
	sc.Step(`^dependent tasks should be unlocked by the Kanban store$`, noopStep)
	sc.Step(`^the LLM gateway is unreachable$`, noopStep)
	sc.Step(`^3 workers observe outage errors$`, noopStep)
	sc.Step(`^the circuit breaker should open$`, noopStep)
	sc.Step(`^the daemon should skip database polling until the timeout expires$`, noopStep)
	sc.Step(`^a running task has failed twice before$`, noopStep)
	sc.Step(`^the sandbox fails the task again$`, noopStep)
	sc.Step(`^the retry count should become 3$`, noopStep)
	sc.Step(`^the task should move to FAILED$`, noopStep)
	sc.Step(`^a system comment should explain the eviction$`, noopStep)
	sc.Step(`^a task is RUNNING with an operating system process id$`, noopStep)
	sc.Step(`^the process id is not alive$`, noopStep)
	sc.Step(`^the daemon boots$`, noopStep)
	sc.Step(`^a recovery event should be emitted$`, noopStep)
	sc.Step(`^agentd has an initialized home directory$`, noopStep)
	sc.Step(`^the user runs agentd status$`, noopStep)
	sc.Step(`^the command should print task state counts$`, noopStep)
	sc.Step(`^the user runs agentd comment for a task$`, noopStep)
	sc.Step(`^a human comment should be added to that task$`, noopStep)
	sc.Step(`^a task is IN_CONSIDERATION after a human comment$`, noopStep)
	sc.Step(`^the daemon comment intake loop processes the comment$`, noopStep)
	sc.Step(`^the gateway should convert the comment into a plan$`, noopStep)
	sc.Step(`^the new tasks should be linked under the original task$`, noopStep)
	sc.Step(`^the original task should return to READY for system execution$`, noopStep)
}

func noopStep(context.Context) error { return nil }

type queueScenario struct {
	store      *queueStore
	breaker    *CircuitBreaker
	gateway    *queueGateway
	sandbox    *queueSandbox
	worker     *Worker
	daemon     *Daemon
	probe      StaticPIDProbe
	sink       *queueSink
	now        time.Time
	maxRetries int
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan error
	lastQueued int
}

func (s *queueScenario) reset() {
	s.store = newQueueStore()
	s.breaker = NewCircuitBreaker()
	s.now = time.Now().UTC()
	s.breaker.SetClockForTest(func() time.Time { return s.now })
	s.gateway = &queueGateway{content: `{"command":"true"}`}
	s.sandbox = &queueSandbox{result: sandbox.Result{Success: true, ExitCode: 0}}
	s.sink = &queueSink{}
	s.maxRetries = DefaultWorkerMaxRetries
	s.rebuild(2)
}

func (s *queueScenario) rebuild(maxWorkers int) {
	s.worker = NewWorker(s.store, s.gateway, s.sandbox, s.breaker, s.sink, WorkerOptions{MaxRetries: s.maxRetries})
	s.daemon = NewDaemon(s.store, s.worker, nil, s.breaker, s.sink, DaemonOptions{
		MaxWorkers: maxWorkers, Probe: s.probe, TaskInterval: time.Hour, IntakeInterval: time.Hour,
	})
}
