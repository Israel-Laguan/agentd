package config

import (
	"testing"

	"github.com/spf13/viper"
)

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

func TestLoadAgenticConfig_Empty(t *testing.T) {
	t.Parallel()
	v := viper.New()
	cfg := loadAgenticConfig(v)
	if len(cfg.ToolCredentials) != 0 {
		t.Fatalf("ToolCredentials = %v, want empty", cfg.ToolCredentials)
	}
}
