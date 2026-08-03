package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wskills "agentd/internal/agent/skills"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestCacheGolden_ByteStableFirstRequest(t *testing.T) {
	t.Parallel()

	task, project, profile := buildTestInputs(t)
	w := buildTestWorker(t)
	scoped := buildScopedCapabilities()
	executor := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)

	runOnce := func() ([]byte, []byte) {
		return executeRequest(t, w, executor, scoped, task, project, profile)
	}

	m1, t1 := runOnce()
	m2, t2 := runOnce()

	assert.Equal(t, m1, m2, "messages must be byte-identical across runs")
	assert.Equal(t, t1, t2, "tools must be byte-identical across runs")

	assertMessages(t, m1)
	assertToolsSorted(t, t1)
}

func buildTestInputs(t *testing.T) (models.Task, models.Project, models.AgentProfile) {
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-1"},
		Title:       "Implement add",
		Description: "Add function in math.go",
		ProjectID:   "proj-1",
	}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "proj-1"}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{}
	return task, project, profile
}

func buildTestWorker(t *testing.T) *Worker {
	w := &Worker{
		retriever: &mockMemoryRetriever{
			memories: []models.Memory{
				{Scope: "LESSON", Symptom: sql.NullString{String: "slow build", Valid: true}, Solution: sql.NullString{String: "cache deps", Valid: true}},
			},
		},
		capabilities: capabilities.NewRegistry(),
		skillLoader:  &wskills.SkillLoader{GlobalDir: t.TempDir()},
		skillRouter:  &wskills.SkillRouter{Threshold: 1.0},
		toolManifest: nil,
	}
	w.capabilities.Register("alpha", &fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{{Name: "alpha_one", Description: "a"}, {Name: "shared", Description: "s"}},
	})
	w.capabilities.Register("zeta", &fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{{Name: "zeta_two", Description: "z"}, {Name: "shared", Description: "s"}, {Name: "alpha_one", Description: "dup"}},
	})
	return w
}

func buildScopedCapabilities() *capabilities.Registry {
	scoped := capabilities.NewRegistry()
	scoped.Register("scoped-adapter", &fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{{Name: "scoped_three", Description: "sc"}},
	})
	return scoped
}

func executeRequest(t *testing.T, w *Worker, executor *agenttools.ToolExecutor, scoped *capabilities.Registry, task models.Task, project models.Project, profile models.AgentProfile) ([]byte, []byte) {
	t.Helper()
	messages := w.assembleAgenticSystemPrompt(context.Background(), task, project, profile)
	tools, _ := w.agenticToolsWithExtras(context.Background(), executor, scoped)
	tools, _ = w.filterAgenticTools(tools, nil, task, profile)
	messagesBytes, err := json.Marshal(messages)
	require.NoError(t, err)
	toolsBytes, err := json.Marshal(tools)
	require.NoError(t, err)
	return messagesBytes, toolsBytes
}

func assertMessages(t *testing.T, m1 []byte) {
	t.Helper()
	if messages := decodeMessages(t, m1); len(messages) >= 3 {
		assert.Equal(t, "system", messages[0].Role, "message[0] must be the layered system prompt")
		assert.Equal(t, "user", messages[1].Role, "message[1] must be the task seed user message")
		assert.Equal(t, "system", messages[2].Role, "message[2] must be the memory lessons system message")
		assert.Contains(t, messages[2].Content, "LESSONS LEARNED")
	}
}

func assertToolsSorted(t *testing.T, t1 []byte) {
	t.Helper()
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
