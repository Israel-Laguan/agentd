package memory

import (
	"context"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"

	"github.com/cucumber/godog"
)

func TestMemoryFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeMemoryScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run memory feature tests")
	}
}

type memoryScenario struct {
	store   *memStore
	gw      *memGateway
	breaker *memBreaker
	sink    *memSink
	lib     *Librarian
	task    models.Task
	homeDir string
}

type recallScenario struct {
	memories []models.Memory
	recalled []models.Memory
	start    time.Time
	elapsed  time.Duration
	hanging  bool
	prefs    []models.Memory
}

type dreamScenario struct {
	store   *dreamTestStore
	gw      *memGateway
	dreamer *DreamAgent
}

type prefScenario struct {
	store    *memStore
	prefUser string
	prefText string
}

func initializeMemoryScenario(sc *godog.ScenarioContext) {
	state := &memoryScenario{}
	recall := &recallScenario{}
	dream := &dreamScenario{}
	pref := &prefScenario{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.store = &memStore{}
		state.gw = &memGateway{}
		state.breaker = &memBreaker{}
		state.sink = &memSink{}
		state.task = models.Task{
			BaseEntity: models.BaseEntity{ID: "task-1"},
			ProjectID:  "proj-1",
		}
		dir := ctx.Value(tempDirKey{})
		if dir != nil {
			state.homeDir = dir.(string)
		} else {
			state.homeDir = "/tmp/agentd-mem-test"
		}
		state.lib = &Librarian{
			Store:   state.store,
			Gateway: state.gw,
			Breaker: state.breaker,
			Sink:    state.sink,
			Cfg: config.LibrarianConfig{
				ChunkChars: 50000, MaxReducePasses: 3,
				FallbackHeadTailChars: 2000, ArchiveGraceDays: 7,
			},
			HomeDir: state.homeDir,
		}
		recall.memories = nil
		recall.recalled = nil
		recall.hanging = false
		recall.prefs = nil
		dream.store = &dreamTestStore{}
		dream.gw = &memGateway{}
		pref.store = state.store
		return ctx, nil
	})

	registerLibrarianSteps(sc, state)
	registerRecallSteps(sc, recall)
	registerDreamSteps(sc, dream)
	registerPreferenceSteps(sc, pref, recall)
}

func registerLibrarianSteps(sc *godog.ScenarioContext, state *memoryScenario) {
	sc.Step(`^a completed task with (\d+) events in the store$`, state.taskWithEvents)
	sc.Step(`^the AI gateway is available$`, state.gatewayAvailable)
	sc.Step(`^the circuit breaker is open$`, state.breakerOpen)
	sc.Step(`^the librarian curates the task$`, state.curateTask)
	sc.Step(`^a LOG_ARCHIVED event should be emitted$`, state.logArchivedEmitted)
	sc.Step(`^no LOG_ARCHIVED event should be emitted$`, state.noLogArchivedEmitted)
	sc.Step(`^a durable memory should be recorded with symptom and solution$`, state.memoryRecordedWithSummary)
	sc.Step(`^a durable memory should be recorded using fallback extraction$`, state.memoryRecordedWithFallback)
	sc.Step(`^a MEMORY_INGESTED event should be emitted$`, state.memoryIngestedEmitted)
	sc.Step(`^the events should be marked as curated$`, state.eventsCurated)
	sc.Step(`^no memory should be recorded$`, state.noMemoryRecorded)
	sc.Step(`^stale archives are cleaned and events purged$`, state.cleanAndPurge)
	sc.Step(`^an EVENTS_PURGED event should be emitted$`, state.eventsPurgedEmitted)
	sc.Step(`^the AI gateway returns an empty symptom and solution$`, state.gatewayReturnsEmpty)
	sc.Step(`^the AI gateway returns junk tokens "([^"]*)" and "([^"]*)"$`, state.gatewayReturnsJunk)
	sc.Step(`^a MEMORY_DISCARDED event should be emitted$`, state.memoryDiscardedEmitted)
}

func registerRecallSteps(sc *godog.ScenarioContext, recall *recallScenario) {
	sc.Step(`^a memory tagged "GLOBAL" with symptom "([^"]*)"$`, recall.addGlobalMemory)
	sc.Step(`^a memory tagged project "([^"]*)" with symptom "([^"]*)"$`, recall.addProjectMemory)
	sc.Step(`^the retriever recalls for project "([^"]*)"$`, recall.recallForProject)
	sc.Step(`^the recall should include "([^"]*)"$`, recall.recallIncludes)
	sc.Step(`^the recall should NOT include "([^"]*)"$`, recall.recallExcludes)
	sc.Step(`^a user preference for user "([^"]*)" with text "([^"]*)"$`, recall.addPreference)
	sc.Step(`^the retriever recalls for user "([^"]*)"$`, recall.recallForUser)
	sc.Step(`^the memory store hangs indefinitely$`, recall.setHanging)
	sc.Step(`^the retriever recalls with a 50ms timeout$`, recall.recallWithTimeout)
	sc.Step(`^the recall should return empty$`, recall.recallEmpty)
	sc.Step(`^the recall should complete within 1 second$`, recall.recallFast)
}

func registerDreamSteps(sc *godog.ScenarioContext, dream *dreamScenario) {
	sc.Step(`^(\d+) memories about "([^"]*)"$`, dream.seedMemories)
	sc.Step(`^the AI gateway returns a merged summary$`, dream.gatewayReturnsMerged)
	sc.Step(`^the dream agent runs$`, dream.runDream)
	sc.Step(`^only (\d+) unsuperseded memory should remain$`, dream.unsupersededCount)
	sc.Step(`^the (\d+) original memories should be superseded$`, dream.originalSuperseded)
}

func registerPreferenceSteps(sc *godog.ScenarioContext, pref *prefScenario, recall *recallScenario) {
	sc.Step(`^a user "([^"]*)" sends preference "([^"]*)"$`, pref.setPreference)
	sc.Step(`^the preference is stored$`, pref.storePreference)
	sc.Step(`^a memory with scope "USER_PREFERENCE" should exist$`, pref.prefMemoryExists)
	sc.Step(`^the memory should contain "([^"]*)"$`, pref.memoryContains)
	sc.Step(`^a stored preference for user "([^"]*)" with text "([^"]*)"$`, recall.addStoredPreference)
}

type tempDirKey struct{}
