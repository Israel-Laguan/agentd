package runtime

import (
	"context"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// taskIDTopicGateway records the TaskID of each drift check.
type taskIDTopicGateway struct {
	topicDriftGateway
	taskIDs []string
}

func (g *taskIDTopicGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.taskIDs = append(g.taskIDs, req.TaskID)
	return g.topicDriftGateway.Generate(ctx, req)
}

// T-057 guard: the topic guard runs across a session, not for one task, so its
// call stays unlabelled (empty TaskID) even when the caller's ctx carries one.
func TestTopicGuardCallStaysUnlabelledInATaskContext(t *testing.T) {
	t.Parallel()

	gw := &taskIDTopicGateway{topicDriftGateway: topicDriftGateway{response: "NO"}}
	guard := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})
	ctx := gateway.WithTaskID(context.Background(), "task-9")

	if _, err := guard.DetectDrift(ctx, "fix the parser", "also fix the lexer", models.AgentProfile{}); err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(gw.taskIDs) != 1 || gw.taskIDs[0] != "" {
		t.Errorf("topic guard TaskIDs = %q, want one empty TaskID", gw.taskIDs)
	}
}
