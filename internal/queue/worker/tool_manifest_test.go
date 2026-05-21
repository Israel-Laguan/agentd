package worker

import (
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func testManifestTools() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: toolNameBash},
		{Name: toolNameRead},
		{Name: toolNameWrite},
		{Name: toolNameDelegate},
		{Name: toolNameDelegateParallel},
		{Name: "capability_tool"},
	}
}

func testManifestIndex() map[string]string {
	return map[string]string{
		toolNameBash:             "builtin",
		toolNameRead:             "builtin",
		toolNameWrite:            "builtin",
		toolNameDelegate:         "builtin",
		toolNameDelegateParallel: "builtin",
		"capability_tool":        "fake",
	}
}

func enabledToolManifest() *ToolManifest {
	return NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
	})
}

func TestTaskClassifier_Summarize(t *testing.T) {
	t.Parallel()
	c := NewTaskClassifier(0.35)
	got := c.Classify(models.Task{
		BaseEntity:  models.BaseEntity{ID: "t1"},
		Title:       "Summarize release notes",
		Description: "Provide a short recap of the changelog",
	})
	if got.Type != TaskTypeSummarize {
		t.Fatalf("Type = %q, want %q", got.Type, TaskTypeSummarize)
	}
	if got.Confidence < 0.35 {
		t.Fatalf("Confidence = %v, want >= 0.35", got.Confidence)
	}
}

func TestTaskClassifier_CodeGen(t *testing.T) {
	t.Parallel()
	c := NewTaskClassifier(0.35)
	got := c.Classify(models.Task{
		BaseEntity:  models.BaseEntity{ID: "t2"},
		Title:       "Fix login bug",
		Description: "Implement patch and add test coverage",
	})
	if got.Type != TaskTypeCodeGen {
		t.Fatalf("Type = %q, want %q (scores=%v)", got.Type, TaskTypeCodeGen, got.Scores)
	}
}

func TestToolManifest_SummarizeZeroTools(t *testing.T) {
	t.Parallel()
	m := enabledToolManifest()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t3"},
		Title:       "Summarize weekly report",
		Description: "Provide a short recap and condense into bullet points",
	}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, models.AgentProfile{})
	if len(tools) != 0 {
		t.Fatalf("tools len = %d, want 0", len(tools))
	}
	if index != nil {
		t.Fatalf("index = %v, want nil", index)
	}
}

func TestToolManifest_CodeGenSubset(t *testing.T) {
	t.Parallel()
	m := enabledToolManifest()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t4"},
		Title:       "Implement feature",
		Description: "Add create handler and build tests",
	}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, models.AgentProfile{})
	if len(tools) != 3 {
		t.Fatalf("tools len = %d, want 3", len(tools))
	}
	for _, name := range []string{toolNameBash, toolNameRead, toolNameWrite} {
		if !containsTool(tools, name) {
			t.Fatalf("missing %q in %v", name, toolNamesFromDefinitions(tools))
		}
	}
	for _, name := range []string{toolNameDelegate, toolNameDelegateParallel, "capability_tool"} {
		if containsTool(tools, name) {
			t.Fatalf("unexpected %q in filtered tools", name)
		}
	}
	if len(index) != 3 {
		t.Fatalf("index len = %d, want 3", len(index))
	}
}

func TestToolManifest_LowConfidenceFallback(t *testing.T) {
	t.Parallel()
	m := enabledToolManifest()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t5"},
		Title:       "Do something",
		Description: "General work item with no strong signals",
	}
	orig := testManifestTools()
	tools, index := m.Filter(orig, testManifestIndex(), task, models.AgentProfile{})
	if len(tools) != len(orig) {
		t.Fatalf("tools len = %d, want %d (full fallback)", len(tools), len(orig))
	}
	if len(index) != len(testManifestIndex()) {
		t.Fatalf("index len = %d, want %d", len(index), len(testManifestIndex()))
	}
}

func TestToolManifest_ProfileAllowedTools(t *testing.T) {
	t.Parallel()
	m := enabledToolManifest()
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "t6"},
		Title:      "Implement everything",
		Description: "Would normally be code_gen",
	}
	profile := models.AgentProfile{AllowedTools: []string{"read"}}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, profile)
	if len(tools) != 1 || tools[0].Name != toolNameRead {
		t.Fatalf("tools = %v, want only read", toolNamesFromDefinitions(tools))
	}
	if len(index) != 1 || index[toolNameRead] != "builtin" {
		t.Fatalf("index = %v, want read→builtin", index)
	}
}

func TestToolManifest_ProfileForcedType(t *testing.T) {
	t.Parallel()
	m := enabledToolManifest()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t7"},
		Title:       "Implement feature",
		Description: "Would classify as code_gen",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeSummarize}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, profile)
	if len(tools) != 0 {
		t.Fatalf("tools len = %d, want 0 for forced summarize", len(tools))
	}
	if index != nil {
		t.Fatalf("index = %v, want nil", index)
	}
}

func TestToolManifest_DisabledReturnsNil(t *testing.T) {
	t.Parallel()
	if NewToolManifest(config.ToolManifestConfig{Enabled: false}) != nil {
		t.Fatal("NewToolManifest(disabled) should return nil")
	}
}

func TestToolManifest_WebResearchUserMapping(t *testing.T) {
	t.Parallel()
	m := NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
		Mappings: map[string][]string{
			TaskTypeWebResearch: {toolNameRead},
		},
	})
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t8"},
		Title:       "Search the web",
		Description: "Fetch URL and browse lookup results",
	}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, models.AgentProfile{})
	if len(tools) != 1 || tools[0].Name != toolNameRead {
		t.Fatalf("tools = %v, want only read", toolNamesFromDefinitions(tools))
	}
	if len(index) != 1 || index[toolNameRead] != "builtin" {
		t.Fatalf("index = %v, want read→builtin", index)
	}
}

func TestToolManifest_FullAgentRestrictedMapping(t *testing.T) {
	t.Parallel()
	m := NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
		Mappings: map[string][]string{
			TaskTypeFullAgent: {toolNameBash, toolNameRead},
		},
	})
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t9"},
		Title:       "General task",
		Description: "Unrelated work",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeFullAgent}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, profile)
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	for _, name := range []string{toolNameBash, toolNameRead} {
		if !containsTool(tools, name) {
			t.Fatalf("missing %q in %v", name, toolNamesFromDefinitions(tools))
		}
	}
	if len(index) != 2 {
		t.Fatalf("index len = %d, want 2", len(index))
	}
}

func TestToolManifest_FullAgentEmptyMapping(t *testing.T) {
	t.Parallel()
	m := NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
		Mappings: map[string][]string{
			TaskTypeFullAgent: []string{},
		},
	})
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "t10"},
		Title:       "General task",
		Description: "Unrelated work",
	}
	profile := models.AgentProfile{ToolManifestType: TaskTypeFullAgent}
	tools, index := m.Filter(testManifestTools(), testManifestIndex(), task, profile)
	if len(tools) != 0 {
		t.Fatalf("tools len = %d, want 0", len(tools))
	}
	if index != nil {
		t.Fatalf("index = %v, want nil", index)
	}
}

func TestFilterAgenticTools_NoManifestPassthrough(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	orig := testManifestTools()
	tools, _ := w.filterAgenticTools(orig, testManifestIndex(), models.Task{}, models.AgentProfile{})
	if len(tools) != len(orig) {
		t.Fatalf("tools len = %d, want passthrough %d", len(tools), len(orig))
	}
}

func TestFilterToolsByNames_EmptyAllowed(t *testing.T) {
	t.Parallel()
	tools, index := filterToolsByNames(testManifestTools(), testManifestIndex(), map[string]bool{})
	if tools != nil || index != nil {
		t.Fatalf("got tools=%v index=%v, want nil,nil", tools, index)
	}
}
