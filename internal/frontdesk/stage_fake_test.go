package frontdesk

import (
	"encoding/json"

	"agentd/internal/stage"
)

// stageOutput shapes a fake's typed answer as an agent returns it: marshalled
// JSON. Fakes answer at the Agent level, so the Contract's validation is not
// applied to them; the real path is covered by the ModelAgent tests.
func stageOutput(v any, err error) (stage.StageResult, error) {
	if err != nil {
		return stage.StageResult{}, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return stage.StageResult{}, err
	}
	return stage.StageResult{Outcome: stage.OutcomeCompleted, Output: raw}, nil
}

// lastMessage is the user message of a stage request, which carries the intent.
func lastMessage(req stage.StageRequest) string {
	return req.Context[len(req.Context)-1].Content
}
