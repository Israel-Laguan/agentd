package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestToolRetriesDefaults_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.MaxAttempts != DefaultToolRetryMaxAttempts {
		t.Fatalf("max_attempts = %d, want %d", cfg.ToolRetries.MaxAttempts, DefaultToolRetryMaxAttempts)
	}
	if cfg.ToolRetries.BaseDelay != DefaultToolRetryBaseDelay {
		t.Fatalf("base_delay = %v, want %v", cfg.ToolRetries.BaseDelay, DefaultToolRetryBaseDelay)
	}
	if cfg.ToolRetries.MaxDelay != DefaultToolRetryMaxDelay {
		t.Fatalf("max_delay = %v, want %v", cfg.ToolRetries.MaxDelay, DefaultToolRetryMaxDelay)
	}
	if !cfg.ToolRetries.Allows("read") {
		t.Fatal("expected read to be in default allowlist")
	}
	if cfg.ToolRetries.Allows("bash") {
		t.Fatal("bash should not be in default allowlist")
	}
}

func TestToolRetriesOverride_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_retries.max_attempts", 5)
	v.Set("queue.tool_retries.base_delay", "500ms")
	v.Set("queue.tool_retries.max_delay", "10s")
	v.Set("queue.tool_retries.tools", []string{"read", "bash"})
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.MaxAttempts != 5 {
		t.Fatalf("max_attempts = %d, want 5", cfg.ToolRetries.MaxAttempts)
	}
	if cfg.ToolRetries.BaseDelay != 500*time.Millisecond {
		t.Fatalf("base_delay = %v, want 500ms", cfg.ToolRetries.BaseDelay)
	}
	if cfg.ToolRetries.MaxDelay != 10*time.Second {
		t.Fatalf("max_delay = %v, want 10s", cfg.ToolRetries.MaxDelay)
	}
	if !cfg.ToolRetries.Allows("read") || !cfg.ToolRetries.Allows("bash") {
		t.Fatal("expected read and bash in allowlist")
	}
	if cfg.ToolRetries.Allows("write") {
		t.Fatal("write should not be in allowlist")
	}
}

func TestToolRetriesConfig_Allows_Empty(t *testing.T) {
	cfg := ToolRetriesConfig{Tools: nil}
	if cfg.Allows("read") {
		t.Fatal("empty allowlist should deny all tools")
	}
}

func TestToolRetriesBareNumericDelay_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_retries.base_delay", 500)
	v.Set("queue.tool_retries.max_delay", 3000)
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.BaseDelay != 500*time.Millisecond {
		t.Fatalf("base_delay = %v, want 500ms", cfg.ToolRetries.BaseDelay)
	}
	if cfg.ToolRetries.MaxDelay != 3*time.Second {
		t.Fatalf("max_delay = %v, want 3s", cfg.ToolRetries.MaxDelay)
	}
}

func TestToolRetriesDurationString_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_retries.base_delay", "1s")
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.BaseDelay != time.Second {
		t.Fatalf("base_delay = %v, want 1s", cfg.ToolRetries.BaseDelay)
	}
}

func TestToolRetriesInvalidDelay_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_retries.base_delay", "not-a-duration")
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.BaseDelay != DefaultToolRetryBaseDelay {
		t.Fatalf("base_delay = %v, want default %v", cfg.ToolRetries.BaseDelay, DefaultToolRetryBaseDelay)
	}
}
