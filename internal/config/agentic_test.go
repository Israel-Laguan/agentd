package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestAgenticDefaults_Viper(t *testing.T) {
	v := viper.New()
	setAgenticDefaults(v)
	cfg := loadAgenticConfig(v)

	if len(cfg.ExternalTools) != 0 {
		t.Fatalf("external_tools = %v, want empty slice", cfg.ExternalTools)
	}
	if cfg.ContextWarningThreshold != DefaultContextWarningThreshold {
		t.Fatalf("ContextWarningThreshold = %v, want %v", cfg.ContextWarningThreshold, DefaultContextWarningThreshold)
	}
	if cfg.ToolFailureStreak != DefaultToolFailureStreak {
		t.Fatalf("ToolFailureStreak = %d, want %d", cfg.ToolFailureStreak, DefaultToolFailureStreak)
	}
	if !cfg.TopicGuard.Enabled {
		t.Fatal("TopicGuard.Enabled should default to true")
	}
	if cfg.TopicGuard.Sensitivity != DefaultTopicGuardSensitivity {
		t.Fatalf("TopicGuard.Sensitivity = %v, want %v", cfg.TopicGuard.Sensitivity, DefaultTopicGuardSensitivity)
	}
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

func TestLoadAgenticConfig_NegativeComplexityThresholdClamped(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setAgenticDefaults(v)
	v.Set("agentic.planning.complexity_threshold", -10)

	cfg := loadAgenticConfig(v)
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

	cfg := loadAgenticConfig(v)
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
	cfg := loadAgenticConfig(v)

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

	cfg := loadAgenticConfig(v)
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

	cfg := loadAgenticConfig(v)
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

	cfg := loadAgenticConfig(v)
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
	cfg := loadAgenticConfig(v)
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
