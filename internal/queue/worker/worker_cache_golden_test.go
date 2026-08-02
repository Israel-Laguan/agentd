package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type fakeRetriever struct {
	memories []models.Memory
}

func (f *fakeRetriever) Recall(ctx context.Context, intent, projectID, userID string) []models.Memory {
	return f.memories
}

type fakeCapabilityAdapter struct {
	name  string
	tools []gateway.ToolDefinition
}

func (f *fakeCapabilityAdapter) Name() string { return f.name }
func (f *fakeCapabilityAdapter) ListTools(ctx context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}
func (f *fakeCapabilityAdapter) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	return nil, nil
}
func (f *fakeCapabilityAdapter) Close() error { return nil }

func TestCacheGolden_ByteStableFirstRequest(t *testing.T) {
	t.Parallel()

	// Stable inputs: same task, profile, project, lessons, and adapters.
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-1"},
		Title:       "Implement add",
		Description: "Add function in math.go",
		ProjectID:   "proj-1",
	}
	project := models.Project{ID: "proj-1", WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{}

	w := &Worker{
		retriever: &fakeRetriever{
			memories: []models.Memory{
				{Scope: "LESSON", Symptom: sql.NullString{String: "slow build", Valid: true}, Solution: sql.NullString{String: "cache deps", Valid: true}},
			},
		},
		capabilities: capabilities.NewRegistry(),
		skillLoader:  &agentruntime.SkillLoader{GlobalDir: t.TempDir()},
		skillRouter:  &agentruntime.SkillRouter{Threshold: 1.0},
		toolManifest: nil,
	}
	// Two adapters contributing overlapping + unique tools; map iteration is
	// intentionally nondeterministic so this guards against map-order leakage.
	w.capabilities.Register("alpha", &fakeCapabilityAdapter{
		name:  "alpha",
		tools: []gateway.ToolDefinition{{Name: "alpha_one", Description: "a"}, {Name: "shared", Description: "s"}},
	})
	w.capabilities.Register("zeta", &fakeCapabilityAdapter{
		name:  "zeta",
		tools: []gateway.ToolDefinition{{Name: "zeta_two", Description: "z"}, {Name: "shared", Description: "s"}, {Name: "alpha_one", Description: "dup"}},
	})

	scoped := capabilities.NewRegistry()
	scoped.Register("scoped-adapter", &fakeCapabilityAdapter{
		name:  "scoped-adapter",
		tools: []gateway.ToolDefinition{{Name: "scoped_three", Description: "sc"}},
	})

	executor := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)

	runOnce := func() ([]byte, []byte) {
		messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)
		tools, _ := w.agenticToolsWithExtras(context.Background(), executor, scoped)
		tools, _ = w.filterAgenticTools(tools, nil, task, profile)
		messagesBytes, err := json.Marshal(messages)
		require.NoError(t, err)
		toolsBytes, err := json.Marshal(tools)
		require.NoError(t, err)
		return messagesBytes, toolsBytes
	}

	m1, t1 := runOnce()
	m2, t2 := runOnce()

	assert.Equal(t, m1, m2, "messages must be byte-identical across runs")
	assert.Equal(t, t1, t2, "tools must be byte-identical across runs")

	// Memory lessons must sit after the stable system prompt + task seed.
	if len(messages := decodeMessages(t, m1); len(messages) >= 3 {
		assert.Equal(t, "system", messages[0].Role, "message[0] must be the layered system prompt")
		assert.Equal(t, "user", messages[1].Role, "message[1] must be the task seed user message")
		assert.Equal(t, "system", messages[len(messages)-1].Role, "last message must be the memory lessons system message")
		assert.Contains(t, messages[len(messages)-1].Content, "LESSONS LEARNED")
	}

	// Tool list must be sorted by name.
	var tools []gateway.ToolDefinition
	require.NoError(t, json.Unmarshal(t1, &tools))
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	assert.True(t, sort.StringsAreSorted(names), "tool names must be sorted canonically: %v", names)
}

func decodeMessages(t *testing.T, b []byte) []gateway.PromptMessage {
	t.Helper()
	var ms []gateway.PromptMessage
	if err := json.Unmarshal(b, &ms); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	return ms
}
