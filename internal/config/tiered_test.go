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
	cpc := cfg.ContextPackConfig()
	if cpc.MaxPaths != DefaultTieredMaxPaths {
		t.Fatalf("ContextPackConfig.MaxPaths = %d, want %d", cpc.MaxPaths, DefaultTieredMaxPaths)
	}
	if cpc.MaxChars != DefaultTieredMaxChars {
		t.Fatalf("ContextPackConfig.MaxChars = %d, want %d", cpc.MaxChars, DefaultTieredMaxChars)
	}
}

func TestTieredConfig_ExplicitValues(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	v.Set("tiered.complexity_threshold", 100)
	v.Set("tiered.context_pack.max_paths", 20)
	v.Set("tiered.context_pack.max_chars", 24000)
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
	cpc := cfg.ContextPackConfig()
	if cpc.MaxPaths != 20 {
		t.Fatalf("ContextPackConfig.MaxPaths = %d, want 20", cpc.MaxPaths)
	}
	if cpc.MaxChars != 24000 {
		t.Fatalf("ContextPackConfig.MaxChars = %d, want 24000", cpc.MaxChars)
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

func TestTieredConfig_ContextPackConfigDefaults(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.Set("tiered.enabled", true)
	cfg := loadTieredConfig(v)
	cpc := cfg.ContextPackConfig()
	if cpc.MaxPaths != DefaultTieredMaxPaths {
		t.Fatalf("ContextPackConfig.MaxPaths = %d, want %d", cpc.MaxPaths, DefaultTieredMaxPaths)
	}
	if cpc.MaxChars != DefaultTieredMaxChars {
		t.Fatalf("ContextPackConfig.MaxChars = %d, want %d", cpc.MaxChars, DefaultTieredMaxChars)
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
