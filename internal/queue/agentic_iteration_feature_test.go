package queue

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestAgenticIterationSafetyFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeAgenticIterationScenario,
		Options: &godog.Options{
			Format:    "pretty",
			Paths:     []string{"features/agentic_iteration_safety.feature"},
			TestingT:  t,
			Strict:    true,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run agentic iteration safety feature tests")
	}
}
