package api_test

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestAPIFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeAPIScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run API feature tests")
	}
}
