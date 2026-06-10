package config

import (
	"math"
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

func TestToolTimeoutsBareNumeric_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_timeouts.bash", 120)
	cfg := loadQueueConfig(v)

	if got := cfg.ToolTimeouts.Lookup("bash", 0); got != 120*time.Second {
		t.Fatalf("bash timeout = %v, want 120s", got)
	}
}

func TestToolTimeoutsDurationString_Viper(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_timeouts.bash", "90s")
	cfg := loadQueueConfig(v)

	if got := cfg.ToolTimeouts.Lookup("bash", 0); got != 90*time.Second {
		t.Fatalf("bash timeout = %v, want 90s", got)
	}
}

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

func TestScaleBareNumber(t *testing.T) {
	t.Parallel()

	fallback := 5 * time.Second
	unit := time.Millisecond

	tests := []struct {
		name string
		n    float64
		want time.Duration
	}{
		{name: "normal int", n: 200, want: 200 * time.Millisecond},
		{name: "normal float", n: 1.5, want: time.Duration(1.5 * float64(time.Millisecond))},
		{name: "zero", n: 0, want: fallback},
		{name: "negative", n: -1, want: fallback},
		{name: "NaN", n: math.NaN(), want: fallback},
		{name: "positive Inf", n: math.Inf(1), want: fallback},
		{name: "overflow int64", n: float64(math.MaxInt64), want: fallback},
		{name: "huge float64", n: 1e20, want: fallback},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scaleBareNumber(tc.n, unit, fallback); got != tc.want {
				t.Fatalf("scaleBareNumber(%v) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestScaleBareNumber_AtLimit(t *testing.T) {
	t.Parallel()

	fallback := time.Second
	unit := time.Millisecond
	maxBare := float64(math.MaxInt64) / float64(unit)
	n := maxBare - 1

	got := scaleBareNumber(n, unit, fallback)
	want := time.Duration(n * float64(unit))
	if got != want {
		t.Fatalf("scaleBareNumber(at limit) = %v, want %v", got, want)
	}
}

func TestParseViperDuration_BareNumericString(t *testing.T) {
	t.Parallel()

	fallback := 200 * time.Millisecond
	unit := time.Millisecond

	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "bare numeric string milliseconds", value: "500", want: 500 * time.Millisecond},
		{name: "bare numeric string seconds", value: "120", want: 120 * time.Millisecond},
		{name: "trimmed bare numeric string", value: " 500 ", want: 500 * time.Millisecond},
		{name: "invalid string uses fallback", value: "not-a-duration", want: fallback},
		{name: "empty string uses fallback", value: "", want: fallback},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertParseViperDurationBareNumericString(t, fallback, unit, tc.value, tc.want)
		})
	}
}

func assertParseViperDurationBareNumericString(t *testing.T, fallback, unit time.Duration, value string, want time.Duration) {
	t.Helper()

	v := viper.New()
	v.Set("delay", value)
	got := parseViperDuration(v, "delay", fallback, unit)
	if got != want {
		t.Fatalf("parseViperDuration(%q) = %v, want %v", value, got, want)
	}
	if !v.IsSet("delay") {
		t.Fatal("expected key to be set")
	}
}

func TestToolRetriesBareNumericString_Viper(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_retries.base_delay", "500")
	v.Set("queue.tool_retries.max_delay", "3000")
	cfg := loadQueueConfig(v)

	if cfg.ToolRetries.BaseDelay != 500*time.Millisecond {
		t.Fatalf("base_delay = %v, want 500ms", cfg.ToolRetries.BaseDelay)
	}
	if cfg.ToolRetries.MaxDelay != 3*time.Second {
		t.Fatalf("max_delay = %v, want 3s", cfg.ToolRetries.MaxDelay)
	}
}

func TestToolTimeoutsBareNumericString_Viper(t *testing.T) {
	t.Parallel()
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.tool_timeouts.bash", "120")
	cfg := loadQueueConfig(v)

	if got := cfg.ToolTimeouts.Lookup("bash", 0); got != 120*time.Second {
		t.Fatalf("bash timeout = %v, want 120s", got)
	}
}
