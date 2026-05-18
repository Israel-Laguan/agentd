package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestToolTimeoutsConfig_Lookup_ExactMatch(t *testing.T) {
	cfg := ToolTimeoutsConfig{
		Defaults: map[string]time.Duration{
			"bash":    60 * time.Second,
			"read":    10 * time.Second,
			"default": 30 * time.Second,
		},
	}
	if got := cfg.Lookup("bash", time.Second); got != 60*time.Second {
		t.Fatalf("Lookup(bash) = %v, want 60s", got)
	}
	if got := cfg.Lookup("read", time.Second); got != 10*time.Second {
		t.Fatalf("Lookup(read) = %v, want 10s", got)
	}
}

func TestToolTimeoutsConfig_Lookup_FallsBackToDefault(t *testing.T) {
	cfg := ToolTimeoutsConfig{
		Defaults: map[string]time.Duration{
			"default": 30 * time.Second,
		},
	}
	if got := cfg.Lookup("unknown_tool", time.Second); got != 30*time.Second {
		t.Fatalf("Lookup(unknown_tool) = %v, want 30s (default key)", got)
	}
}

func TestToolTimeoutsConfig_Lookup_FallsBackToHardcoded(t *testing.T) {
	cfg := ToolTimeoutsConfig{
		Defaults: map[string]time.Duration{
			"bash": 60 * time.Second,
		},
	}
	fallback := 99 * time.Second
	if got := cfg.Lookup("unknown_tool", fallback); got != fallback {
		t.Fatalf("Lookup(unknown_tool) = %v, want %v (hardcoded fallback)", got, fallback)
	}
}

func TestToolTimeoutsConfig_Lookup_NilMap(t *testing.T) {
	cfg := ToolTimeoutsConfig{}
	fallback := 42 * time.Second
	if got := cfg.Lookup("bash", fallback); got != fallback {
		t.Fatalf("Lookup on nil map = %v, want %v", got, fallback)
	}
}

func TestToolTimeoutsDefaults_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	cfg := loadQueueConfig(v)

	if got := cfg.ToolTimeouts.Lookup("bash", 0); got != DefaultBashToolTimeout {
		t.Fatalf("bash timeout = %v, want %v", got, DefaultBashToolTimeout)
	}
	if got := cfg.ToolTimeouts.Lookup("read", 0); got != DefaultReadToolTimeout {
		t.Fatalf("read timeout = %v, want %v", got, DefaultReadToolTimeout)
	}
	if got := cfg.ToolTimeouts.Lookup("write", 0); got != DefaultWriteToolTimeout {
		t.Fatalf("write timeout = %v, want %v", got, DefaultWriteToolTimeout)
	}
	if got := cfg.ToolTimeouts.Lookup("default", 0); got != DefaultToolTimeout {
		t.Fatalf("default timeout = %v, want %v", got, DefaultToolTimeout)
	}
}

func TestToolTimeoutsOverride_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_timeouts.bash", "120s")
	cfg := loadQueueConfig(v)

	if got := cfg.ToolTimeouts.Lookup("bash", 0); got != 120*time.Second {
		t.Fatalf("bash timeout = %v, want 120s", got)
	}
	if got := cfg.ToolTimeouts.Lookup("read", 0); got != DefaultReadToolTimeout {
		t.Fatalf("read timeout should still be default, got %v", got)
	}
}
