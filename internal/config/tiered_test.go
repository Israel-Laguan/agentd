package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestTieredConfig_Defaults(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setTieredDefaults(v)
	cfg := loadTieredConfig(v)
	if cfg.Enabled {
		t.Fatal("Enabled should default to false")
	}
	if cfg.ComplexityThreshold != DefaultTieredComplexityThreshold {
		t.Fatalf("ComplexityThreshold = %d, want %d", cfg.ComplexityThreshold, DefaultTieredComplexityThreshold)
	}
	if cfg.ContextPack.MaxPaths != DefaultTieredMaxPaths {
		t.Fatalf("ContextPack.MaxPaths = %d, want %d", cfg.ContextPack.MaxPaths, DefaultTieredMaxPaths)
	}
	if cfg.ContextPack.MaxChars != DefaultTieredMaxChars {
		t.Fatalf("ContextPack.MaxChars = %d, want %d", cfg.ContextPack.MaxChars, DefaultTieredMaxChars)
	}
	if len(cfg.Models) != 0 {
		t.Fatalf("Models should be empty by default, got %d entries", len(cfg.Models))
	}
}

func TestTieredConfig_ExplicitValues(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	v.Set("tiered.complexity_threshold", 100)
	v.Set("tiered.context_pack.max_paths", 20)
	v.Set("tiered.context_pack.max_chars", 24000)
	v.Set("tiered.models.context.provider", "ollama")
	v.Set("tiered.models.context.model", "llama3:8b")
	v.Set("tiered.models.execute.provider", "ollama")
	v.Set("tiered.models.execute.model", "llama3:8b")
	cfg := loadTieredConfig(v)
	if !cfg.Enabled {
		t.Fatal("Enabled should be true")
	}
	if cfg.ComplexityThreshold != 100 {
		t.Fatalf("ComplexityThreshold = %d, want 100", cfg.ComplexityThreshold)
	}
	if cfg.ContextPack.MaxPaths != 20 {
		t.Fatalf("ContextPack.MaxPaths = %d, want 20", cfg.ContextPack.MaxPaths)
	}
	if cfg.ContextPack.MaxChars != 24000 {
		t.Fatalf("ContextPack.MaxChars = %d, want 24000", cfg.ContextPack.MaxChars)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("Models should have 2 entries, got %d", len(cfg.Models))
	}
	ctx, ok := cfg.Models["context"]
	if !ok {
		t.Fatal("Models missing 'context'")
	}
	if ctx.Provider != "ollama" || ctx.Model != "llama3:8b" {
		t.Fatalf("context model = %+v, want {ollama llama3:8b}", ctx)
	}
}

func TestTieredConfig_NegativeThresholdClamped(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	v.Set("tiered.complexity_threshold", -5)
	cfg := loadTieredConfig(v)
	if cfg.ComplexityThreshold != 0 {
		t.Fatalf("ComplexityThreshold = %d, want 0 (clamped from -5)", cfg.ComplexityThreshold)
	}
}

func TestTieredConfig_ThresholdZeroDisablesSplitting(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	v.Set("tiered.complexity_threshold", 0)
	cfg := loadTieredConfig(v)
	if !cfg.Enabled {
		t.Fatal("Enabled should be true")
	}
	if cfg.ComplexityThreshold != 0 {
		t.Fatalf("ComplexityThreshold = %d, want 0 (disables splitting)", cfg.ComplexityThreshold)
	}
}

func TestTieredConfig_PartialModels(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	v.Set("tiered.models.decision.provider", "gemini")
	v.Set("tiered.models.decision.model", "gemini-2.5-flash")
	cfg := loadTieredConfig(v)
	if len(cfg.Models) != 1 {
		t.Fatalf("Models should have 1 entry, got %d", len(cfg.Models))
	}
	dec, ok := cfg.Models["decision"]
	if !ok {
		t.Fatal("Models missing 'decision'")
	}
	if dec.Provider != "gemini" || dec.Model != "gemini-2.5-flash" {
		t.Fatalf("decision model = %+v, want {gemini gemini-2.5-flash}", dec)
	}
}

func TestSetTieredDefaults_WiredIntoViper(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setTieredDefaults(v)
	if v.GetBool("tiered.enabled") != false {
		t.Fatal("viper default tiered.enabled should be false")
	}
	if v.GetInt("tiered.complexity_threshold") != 200 {
		t.Fatal("viper default tiered.complexity_threshold should be 200")
	}
	if v.GetInt("tiered.context_pack.max_paths") != 40 {
		t.Fatal("viper default tiered.context_pack.max_paths should be 40")
	}
	if v.GetInt("tiered.context_pack.max_chars") != 48000 {
		t.Fatal("viper default tiered.context_pack.max_chars should be 48000")
	}
}
