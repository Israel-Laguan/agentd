package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func mustLoadAgenticConfig(t *testing.T, v *viper.Viper) AgenticConfig {
	t.Helper()
	cfg, err := loadAgenticConfig(v)
	if err != nil {
		t.Fatalf("loadAgenticConfig: %v", err)
	}
	return cfg
}

func assertAgenticCoreDefaults(t *testing.T, cfg AgenticConfig) {
	t.Helper()
	if len(cfg.ExternalTools) != 0 {
		t.Fatalf("external_tools = %v, want empty slice", cfg.ExternalTools)
	}
	if cfg.ContextWarningThreshold != DefaultContextWarningThreshold {
		t.Fatalf("ContextWarningThreshold = %v, want %v", cfg.ContextWarningThreshold, DefaultContextWarningThreshold)
	}
	if cfg.ToolFailureStreak != DefaultToolFailureStreak {
		t.Fatalf("ToolFailureStreak = %d, want %d", cfg.ToolFailureStreak, DefaultToolFailureStreak)
	}
}

func assertAgenticTopicGuardDefaults(t *testing.T, cfg AgenticConfig) {
	t.Helper()
	if !cfg.TopicGuard.Enabled {
		t.Fatal("TopicGuard.Enabled should default to true")
	}
	if cfg.TopicGuard.Sensitivity != DefaultTopicGuardSensitivity {
		t.Fatalf("TopicGuard.Sensitivity = %v, want %v", cfg.TopicGuard.Sensitivity, DefaultTopicGuardSensitivity)
	}
}

func assertAgenticPlanningDefaults(t *testing.T, cfg AgenticConfig) {
	t.Helper()
	if cfg.Planning.ComplexityThreshold != 0 {
		t.Fatalf("Planning.ComplexityThreshold = %d, want 0", cfg.Planning.ComplexityThreshold)
	}
	if cfg.Planning.MaxRedoPasses != DefaultPlanningMaxRedoPasses {
		t.Fatalf("Planning.MaxRedoPasses = %d, want %d", cfg.Planning.MaxRedoPasses, DefaultPlanningMaxRedoPasses)
	}
	if cfg.Planning.PlanContextMaxChars != DefaultPlanContextMaxChars {
		t.Fatalf("Planning.PlanContextMaxChars = %d, want %d", cfg.Planning.PlanContextMaxChars, DefaultPlanContextMaxChars)
	}
}

func assertAgenticToolManifestDefaults(t *testing.T, cfg AgenticConfig) {
	t.Helper()
	if cfg.ToolManifest.Enabled {
		t.Fatal("ToolManifest.Enabled should default to false")
	}
	if cfg.ToolManifest.MinConfidence != DefaultToolManifestMinConfidence {
		t.Fatalf("ToolManifest.MinConfidence = %v, want %v",
			cfg.ToolManifest.MinConfidence, DefaultToolManifestMinConfidence)
	}
}

func assertAgenticBatchingDefaults(t *testing.T, cfg AgenticConfig) {
	t.Helper()
	if cfg.Batching.Enabled {
		t.Fatal("Batching.Enabled should default to false")
	}
	if cfg.Batching.MaxBatchSize != DefaultBatchingMaxBatchSize {
		t.Fatalf("Batching.MaxBatchSize = %d, want %d", cfg.Batching.MaxBatchSize, DefaultBatchingMaxBatchSize)
	}
}

func TestAgenticDefaults_Viper(t *testing.T) {
	v := viper.New()
	setAgenticDefaults(v)
	cfg := mustLoadAgenticConfig(t, v)

	assertAgenticCoreDefaults(t, cfg)
	assertAgenticTopicGuardDefaults(t, cfg)
	assertAgenticPlanningDefaults(t, cfg)
	assertAgenticToolManifestDefaults(t, cfg)
	assertAgenticBatchingDefaults(t, cfg)
}

func TestLoadAgenticConfig_BatchingOverride(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.batching.enabled", true)
	v.Set("agentic.batching.max_batch_size", 3)

	cfg := mustLoadAgenticConfig(t, v)
	if !cfg.Batching.Enabled {
		t.Fatal("Batching.Enabled = false, want true")
	}
	if cfg.Batching.MaxBatchSize != 3 {
		t.Fatalf("Batching.MaxBatchSize = %d, want 3", cfg.Batching.MaxBatchSize)
	}
}

func TestLoadAgenticConfig_BatchingInvalidMaxFallsBack(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.batching.enabled", true)
	v.Set("agentic.batching.max_batch_size", 0)

	cfg := mustLoadAgenticConfig(t, v)
	if cfg.Batching.MaxBatchSize != DefaultBatchingMaxBatchSize {
		t.Fatalf("Batching.MaxBatchSize = %d, want %d", cfg.Batching.MaxBatchSize, DefaultBatchingMaxBatchSize)
	}

	v.Set("agentic.batching.max_batch_size", -1)
	cfg = mustLoadAgenticConfig(t, v)
	if cfg.Batching.MaxBatchSize != DefaultBatchingMaxBatchSize {
		t.Fatalf("negative max: MaxBatchSize = %d, want %d", cfg.Batching.MaxBatchSize, DefaultBatchingMaxBatchSize)
	}
}

func TestLoadAgenticConfig_BatchingMaxClampedToUpperBound(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.batching.max_batch_size", 999)

	cfg := mustLoadAgenticConfig(t, v)
	if cfg.Batching.MaxBatchSize != MaxBatchingMaxBatchSize {
		t.Fatalf("Batching.MaxBatchSize = %d, want %d", cfg.Batching.MaxBatchSize, MaxBatchingMaxBatchSize)
	}
}

func TestLoadAgenticConfig_ToolManifestOverride(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.tool_manifest.enabled", true)
	v.Set("agentic.tool_manifest.min_confidence", 0.5)
	v.Set("agentic.tool_manifest.mappings", map[string]interface{}{
		"summarize": []interface{}{},
		"code_gen":  []interface{}{"bash", "read", "write"},
		"doc_qa":    []interface{}{"read"},
	})

	cfg := mustLoadAgenticConfig(t, v)
	if !cfg.ToolManifest.Enabled {
		t.Fatal("ToolManifest.Enabled = false, want true")
	}
	if cfg.ToolManifest.MinConfidence != 0.5 {
		t.Fatalf("MinConfidence = %v, want 0.5", cfg.ToolManifest.MinConfidence)
	}
	if len(cfg.ToolManifest.Mappings["summarize"]) != 0 {
		t.Fatalf("summarize mapping = %v, want empty", cfg.ToolManifest.Mappings["summarize"])
	}
	if len(cfg.ToolManifest.Mappings["code_gen"]) != 3 {
		t.Fatalf("code_gen mapping = %v, want 3 tools", cfg.ToolManifest.Mappings["code_gen"])
	}
	if cfg.ToolManifest.Mappings["doc_qa"][0] != "read" {
		t.Fatalf("doc_qa mapping = %v, want [read]", cfg.ToolManifest.Mappings["doc_qa"])
	}
}

func TestLoadAgenticConfig_NegativeComplexityThresholdClamped(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.planning.complexity_threshold", -10)

	cfg := mustLoadAgenticConfig(t, v)
	if cfg.Planning.ComplexityThreshold != 0 {
		t.Fatalf("ComplexityThreshold = %d, want 0", cfg.Planning.ComplexityThreshold)
	}
}

func TestLoadAgenticConfig_PlanningOverride(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.planning.complexity_threshold", 500)
	v.Set("agentic.planning.max_redo_passes", 5)
	v.Set("agentic.planning.plan_context_max_chars", 2000)

	cfg := mustLoadAgenticConfig(t, v)
	if cfg.Planning.ComplexityThreshold != 500 {
		t.Fatalf("ComplexityThreshold = %d, want 500", cfg.Planning.ComplexityThreshold)
	}
	if cfg.Planning.MaxRedoPasses != 5 {
		t.Fatalf("MaxRedoPasses = %d, want 5", cfg.Planning.MaxRedoPasses)
	}
	if cfg.Planning.PlanContextMaxChars != 2000 {
		t.Fatalf("PlanContextMaxChars = %d, want 2000", cfg.Planning.PlanContextMaxChars)
	}
}

func TestAgenticExternalToolsOverride_Viper(t *testing.T) {
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.external_tools", []string{"web_fetch", "search"})
	cfg := mustLoadAgenticConfig(t, v)

	if len(cfg.ExternalTools) != 2 {
		t.Fatalf("external_tools len = %d, want 2", len(cfg.ExternalTools))
	}
	if cfg.ExternalTools[0] != "web_fetch" || cfg.ExternalTools[1] != "search" {
		t.Fatalf("external_tools = %v, want [web_fetch search]", cfg.ExternalTools)
	}
}

func TestLoadAgenticConfig_ToolCredentials(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("agentic.tool_credentials.github", "GITHUB_TOKEN")
	v.Set("agentic.tool_credentials.jira", "JIRA_API_KEY")

	cfg := mustLoadAgenticConfig(t, v)
	if cfg.ToolCredentials["github"] != "GITHUB_TOKEN" {
		t.Fatalf("github = %q, want GITHUB_TOKEN", cfg.ToolCredentials["github"])
	}
	if cfg.ToolCredentials["jira"] != "JIRA_API_KEY" {
		t.Fatalf("jira = %q, want JIRA_API_KEY", cfg.ToolCredentials["jira"])
	}
}

func TestLoadAgenticConfig_DisableCredentialDetection(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("agentic.disable_credential_detection", true)

	cfg := mustLoadAgenticConfig(t, v)
	if !cfg.DisableCredentialDetection {
		t.Fatal("DisableCredentialDetection = false, want true")
	}
}

func TestLoadAgenticConfig_Audit(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.audit.enabled", true)
	v.Set("agentic.audit.path", "/var/log/agentd/audit.jsonl")

	cfg := mustLoadAgenticConfig(t, v)
	if !cfg.Audit.Enabled {
		t.Fatal("Audit.Enabled = false, want true")
	}
	if cfg.Audit.Path != "/var/log/agentd/audit.jsonl" {
		t.Fatalf("Audit.Path = %q, want absolute path", cfg.Audit.Path)
	}
}

func TestResolveAuditPath_Relative(t *testing.T) {
	t.Parallel()
	got := ResolveAuditPath("/home/agentd", "audit.jsonl")
	want := filepath.Join("/home/agentd", "audit.jsonl")
	if got != want {
		t.Fatalf("ResolveAuditPath = %q, want %q", got, want)
	}
}

func TestLoadAgenticConfig_Empty(t *testing.T) {
	t.Parallel()
	v := viper.New()
	cfg := mustLoadAgenticConfig(t, v)
	if len(cfg.ToolCredentials) != 0 {
		t.Fatalf("ToolCredentials = %v, want empty", cfg.ToolCredentials)
	}
	if cfg.DisableCredentialDetection {
		t.Fatal("DisableCredentialDetection = true, want false by default")
	}
	if cfg.Audit.Enabled {
		t.Fatal("Audit.Enabled = true, want false by default")
	}
}
