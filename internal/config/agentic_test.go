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
