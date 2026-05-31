package runtime

import (
	"context"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type topicDriftGateway struct {
	response string
	calls    int
}

func (g *topicDriftGateway) Generate(_ context.Context, _ gateway.AIRequest) (gateway.AIResponse, error) {
	g.calls++
	return gateway.AIResponse{Content: g.response}, nil
}

func (g *topicDriftGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}

func (g *topicDriftGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (g *topicDriftGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

func (g *topicDriftGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func TestTopicGuard_CSSThenDatabaseMigrations_Drift(t *testing.T) {
	gw := &topicDriftGateway{response: "YES"}
	tg := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true, Sensitivity: 0.5})

	drift, err := tg.DetectDrift(context.Background(),
		"Help me style the login page with CSS flexbox",
		"How do I run database migrations for PostgreSQL?",
		models.AgentProfile{},
	)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if !drift {
		t.Fatal("expected drift for CSS styling vs database migrations")
	}
	if gw.calls != 1 {
		t.Fatalf("expected 1 gateway call, got %d", gw.calls)
	}
}

func TestTopicGuard_MigrationFollowUp_NoDrift(t *testing.T) {
	gw := &topicDriftGateway{response: "NO"}
	tg := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true, Sensitivity: 0.5})

	drift, err := tg.DetectDrift(context.Background(),
		"How do I run database migrations for PostgreSQL?",
		"now add tests for the migration",
		models.AgentProfile{},
	)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if drift {
		t.Fatal("expected no drift for migration follow-up")
	}
}

func TestTopicGuard_DisabledGlobally(t *testing.T) {
	gw := &topicDriftGateway{response: "YES"}
	tg := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: false})

	drift, err := tg.DetectDrift(context.Background(), "a", "b", models.AgentProfile{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if drift {
		t.Fatal("expected no drift when globally disabled")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.calls)
	}
}

func TestTopicGuard_DisabledPerProfile(t *testing.T) {
	gw := &topicDriftGateway{response: "YES"}
	tg := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})

	drift, err := tg.DetectDrift(context.Background(), "a", "b", models.AgentProfile{DisableTopicDrift: true})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if drift {
		t.Fatal("expected no drift when disabled on profile")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.calls)
	}
}

func TestTopicGuard_EmptyInput(t *testing.T) {
	gw := &topicDriftGateway{response: "YES"}
	tg := NewTopicGuard(gw, config.TopicGuardConfig{Enabled: true})

	drift, _ := tg.DetectDrift(context.Background(), "", "new", models.AgentProfile{})
	if drift {
		t.Fatal("expected no drift with empty session topic")
	}
	drift, _ = tg.DetectDrift(context.Background(), "topic", "", models.AgentProfile{})
	if drift {
		t.Fatal("expected no drift with empty new input")
	}
	if gw.calls != 0 {
		t.Fatalf("expected no gateway calls, got %d", gw.calls)
	}
}

func TestParseTopicDriftResponse(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"YES", true},
		{"yes", true},
		{"YES.", true},
		{"NO", false},
		{"no, related", false},
		{"maybe", false},
	}
	for _, tc := range cases {
		if got := parseTopicDriftResponse(tc.in); got != tc.want {
			t.Errorf("parseTopicDriftResponse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
