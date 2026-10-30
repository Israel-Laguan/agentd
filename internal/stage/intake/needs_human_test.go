package intake

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
)

// A reply that only asks for a person is not an answer. The intake calls must
// report stage.ErrNeedsHuman, not decode it into an empty plan, an empty scope
// or an "ambiguous" intent that callers would treat as a result.
func TestIntakeReportsNeedsHumanInsteadOfAnEmptyResult(t *testing.T) {
	t.Parallel()
	const reply = `{"needs_human":true,"question":"Which client?"}`
	calls := map[string]func(stage.Agent) error{
		"plan": func(a stage.Agent) error {
			_, err := Plan(context.Background(), a, spec.PurposeIntakeDraftPlan, 5, "build it")
			return err
		},
		"scope": func(a stage.Agent) error {
			_, err := AnalyzeScope(context.Background(), a, "build it")
			return err
		},
		"intent": func(a stage.Agent) error {
			_, err := ClassifyIntent(context.Background(), a, "build it")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			agent := stage.NewModelAgent(&fakeGateway{replies: []string{reply}})
			if err := call(agent); !errors.Is(err, stage.ErrNeedsHuman) {
				t.Fatalf("%s error = %v, want stage.ErrNeedsHuman", name, err)
			}
		})
	}
}
