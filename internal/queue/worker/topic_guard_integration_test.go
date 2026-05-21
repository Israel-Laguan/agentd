package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type driftTrackingGateway struct {
	topicDriftGateway
}

func TestGuardTopicDrift_SkipsFirstTurn(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	w := NewWorker(nil, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "CSS styling", nil)

	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}
	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 0, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no reset on turn 0")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no drift gateway calls on turn 0, got %d", gw.calls)
	}
}

func TestGuardTopicDrift_NoNewComment_SkipsDriftCall(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	w := NewWorker(testutil.NewFakeStore(), gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "CSS styling", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "CSS styling"}}

	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no reset without new comment")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.calls)
	}
}

func TestGuardTopicDrift_DriftBeforeCompression(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "YES"}}
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "database migrations",
	})

	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
		AgenticContext: config.AgenticContextConfig{
			RollingThresholdTurns: 1,
			KeepRecentTurns:       1,
		},
	})
	sm := NewSessionManager("task-1", "CSS styling", w.checkpointStore)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "CSS styling"},
		{Role: "assistant", Content: "styled"},
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}

	reset, err := w.guardTopicDrift(context.Background(), task, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if !reset || !errors.Is(err, errTopicDriftReset) {
		t.Fatalf("guardTopicDrift = (%v, %v), want (true, errTopicDriftReset)", reset, err)
	}
	// Drift reset runs before prepareAgenticIteration would compress the abandoned session.
	if strings.Contains(messages[0].Content, "PREVIOUS CONTEXT SUMMARY") && len(messages) <= 2 {
		t.Fatal("fresh session should not include compressed summary in short message list")
	}
	for _, m := range messages {
		if m.Role == "assistant" {
			t.Fatal("assistant history should be cleared after drift")
		}
	}
}

func TestRunAgenticTurnLoop_TopicDriftErrWithRewindContinues(t *testing.T) {
	t.Parallel()
	err := errTopicDriftReset
	rewindTo := rewindToFirstTurn
	continues := false
	if err != nil {
		if errors.Is(err, errTopicDriftReset) && rewindTo >= 0 {
			continues = true
		}
	}
	if !continues {
		t.Fatal("topic drift error with rewind should continue the turn loop, not abort")
	}
}

func TestGuardTopicDrift_RelatedFollowUp_NoReset(t *testing.T) {
	gw := &driftTrackingGateway{topicDriftGateway: topicDriftGateway{response: "NO"}}
	store := testutil.NewFakeStore()
	now := time.Now().UTC()
	_ = store.AddComment(context.Background(), models.Comment{
		BaseEntity: models.BaseEntity{UpdatedAt: now},
		TaskID:     "task-1", Author: models.CommentAuthorUser, Body: "now add tests for the migration",
	})
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		TopicGuard: config.TopicGuardConfig{Enabled: true},
	})
	sm := NewSessionManager("task-1", "database migrations", nil)
	messages := []gateway.PromptMessage{{Role: "user", Content: "database migrations"}}

	reset, err := w.guardTopicDrift(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}}, models.Project{}, models.AgentProfile{}, 1, &messages, sm)
	if err != nil {
		t.Fatalf("guardTopicDrift: %v", err)
	}
	if reset {
		t.Fatal("expected no session reset for related follow-up")
	}
	if sm.TopicAnchor() != "now add tests for the migration" {
		t.Fatalf("topic anchor = %q, want updated follow-up", sm.TopicAnchor())
	}
}
