package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// legacyContentGateway returns a fixed JSON payload for legacy routing tests.
type legacyContentGateway struct {
	routingTestGateway
	content string
}

func (g *legacyContentGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	return gateway.AIResponse{Content: g.content}, nil
}

func legacyCapOptions() WorkerOptions {
	return WorkerOptions{
		MaxToolIterations: 5,
		Legacy: config.LegacyConfig{
			MaxBreakdownDepth:       2,
			MaxSubtasksPerBreakdown: 5,
			PreflightScore:          6,
			RejectScore:             9,
			MaxDescriptionLen:       2000,
		},
	}
}

func TestLegacyBreakdown_SubtaskCountCap_EmitsHandoff(t *testing.T) {
	t.Parallel()

	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-many-subs"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"},{"title":"c","description":"3"},{"title":"d","description":"4"},{"title":"e","description":"5"},{"title":"f","description":"6"}]}`}
	opts := legacyCapOptions()
	opts.Legacy.MaxSubtasksPerBreakdown = 5

	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, opts)
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff when subtask count exceeds cap")
	}
}

func TestLegacyBreakdown_DepthCap_EmitsHandoff(t *testing.T) {
	t.Parallel()

	parent := models.Task{BaseEntity: models.BaseEntity{ID: "parent-1"}, Title: "parent"}
	grand := models.Task{BaseEntity: models.BaseEntity{ID: "grand-1"}, Title: "grand"}
	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-deep"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
			parents: map[string][]models.Task{
				"task-deep": {parent},
				"parent-1":  {grand},
			},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"x","description":"y"}]}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff when breakdown depth exceeds cap")
	}
}

func TestLegacyBreakdown_UnderCap_CreatesSubtasks(t *testing.T) {
	t.Parallel()

	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-ok"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"}]}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	subtasks := store.subtasks
	store.mu.Unlock()
	if len(subtasks) != 2 {
		t.Fatalf("subtasks = %d, want 2", len(subtasks))
	}
	for i, st := range subtasks {
		if st.Assignee != models.TaskAssigneeSystem {
			t.Fatalf("subtask[%d] assignee = %q, want SYSTEM (got agentic handoff?)", i, st.Assignee)
		}
	}
}

func TestLegacyPreflight_AddsNoteForHighComplexity(t *testing.T) {
	t.Parallel()

	w := &Worker{legacyPreflightScore: 6}
	task := models.Task{
		Description: "architect compare design analyse a microservice migration strategy",
	}
	profile := models.AgentProfile{Provider: "ollama", Model: "llama3"}
	msgs := w.legacySeedMessages(task, models.Project{}, profile)
	found := false
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "LEGACY MODE CONSTRAINT") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected legacy preflight user note for high-complexity task")
	}
}

func TestLegacyPreflight_SkipsWhenCustomSystemPrompt(t *testing.T) {
	t.Parallel()

	w := &Worker{legacyPreflightScore: 6}
	task := models.Task{
		Description: "architect compare design analyse a microservice migration strategy",
	}
	profile := models.AgentProfile{
		Provider:     "ollama",
		Model:        "llama3",
		SystemPrompt: sql.NullString{String: "Break it into smaller independently executable subtasks.", Valid: true},
	}
	msgs := w.legacySeedMessages(task, models.Project{}, profile)
	for _, m := range msgs {
		if strings.Contains(m.Content, "LEGACY MODE CONSTRAINT") {
			t.Fatalf("preflight note must not be added when SystemPrompt is set; got %q", m.Content)
		}
	}
}

func TestLegacyDispatchReject_HighComplexitySkipsGateway(t *testing.T) {
	t.Parallel()

	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-reject"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
				Description: "architect compare design analyse reason write draft generate migration plan",
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: false},
		},
	}
	gw := &legacyContentGateway{content: `{"command":"echo ok"}`}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.Process(context.Background(), store.task)

	if len(gw.requests) != 0 {
		t.Fatalf("expected dispatch reject to skip gateway, got %d requests", len(gw.requests))
	}
	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff for high-complexity dispatch reject")
	}
}

func TestLegacyDispatchReject_AgenticCapableProviderStillHandoffs(t *testing.T) {
	t.Parallel()

	router, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{toolCapableConfig("openai", "openai")})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity:  models.BaseEntity{ID: "task-reject-openai"},
				ProjectID:   "project-1",
				AgentID:     "agent-1",
				State:       models.TaskStateQueued,
				Description: "architect compare design analyse reason write draft generate migration plan",
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{
				ID: "agent-1", Provider: "openai", Model: "gpt-4o-mini", AgenticMode: false,
			},
		},
	}
	gw := &legacyContentGateway{
		routingTestGateway: routingTestGateway{router: router},
		content:            `{"command":"echo ok"}`,
	}
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.Process(context.Background(), store.task)

	if len(gw.requests) != 0 {
		t.Fatalf("expected dispatch reject to skip gateway, got %d requests", len(gw.requests))
	}
	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff for high-complexity legacy task on agentic-capable provider")
	}
}

func TestHandleHealingSplit_SubtaskCap_EmitsHealingHandoff(t *testing.T) {
	t.Parallel()

	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-heal-cap"},
				ProjectID:  "project-1",
				AgentID:    "agent-1",
				State:      models.TaskStateQueued,
				RetryCount: 3,
			},
			project: models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}},
			profile: models.AgentProfile{
				ID: "agent-1", Provider: "ollama", Model: "llama3", AgenticMode: true,
			},
		},
	}
	gw := &legacyContentGateway{content: `{"too_complex":true,"subtasks":[{"title":"a","description":"1"},{"title":"b","description":"2"},{"title":"c","description":"3"},{"title":"d","description":"4"},{"title":"e","description":"5"},{"title":"f","description":"6"}]}`}
	sink := &legacyHandoffSink{}
	opts := legacyCapOptions()
	opts.Legacy.MaxSubtasksPerBreakdown = 5
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, sink, opts)

	w.handleHealingSplit(context.Background(), store.task, store.project, store.profile)

	store.mu.Lock()
	blocked := store.blocked
	store.mu.Unlock()
	if !blocked {
		t.Fatal("expected handoff when healing split exceeds subtask cap")
	}
	sink.mu.Lock()
	kinds := sink.kinds
	sink.mu.Unlock()
	if !containsEvent(kinds, "HEALING_HANDOFF") {
		t.Fatalf("expected HEALING_HANDOFF event; got %v", kinds)
	}
	if containsEvent(kinds, "LEGACY_MODE_HANDOFF") {
		t.Fatalf("unexpected LEGACY_MODE_HANDOFF event; got %v", kinds)
	}
}
