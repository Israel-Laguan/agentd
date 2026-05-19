package config

import (
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
}
