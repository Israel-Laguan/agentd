package gateway

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestGatewayFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeGatewayScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run gateway feature tests")
	}
}
