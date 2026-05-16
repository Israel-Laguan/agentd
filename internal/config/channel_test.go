package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestChannelDefaults(t *testing.T) {
	v := viper.New()
	setChannelDefaults(v)
	cfg := loadChannelConfig(v)

	if cfg.MaxMessageSize != DefaultMaxMessageSize {
		t.Fatalf("MaxMessageSize = %d, want %d", cfg.MaxMessageSize, DefaultMaxMessageSize)
	}
	if cfg.RateLimit != DefaultChannelRateLimit {
		t.Fatalf("RateLimit = %d, want %d", cfg.RateLimit, DefaultChannelRateLimit)
	}
	if cfg.RateWindow != DefaultChannelRateWindow {
		t.Fatalf("RateWindow = %d, want %d", cfg.RateWindow, DefaultChannelRateWindow)
	}
}

func TestChannelConfigOverride(t *testing.T) {
	v := viper.New()
	setChannelDefaults(v)
	v.Set("channel.max_message_size", 512)
	v.Set("channel.rate_limit", 10)
	v.Set("channel.rate_window", 30)
	cfg := loadChannelConfig(v)

	if cfg.MaxMessageSize != 512 {
		t.Fatalf("MaxMessageSize = %d, want 512", cfg.MaxMessageSize)
	}
	if cfg.RateLimit != 10 {
		t.Fatalf("RateLimit = %d, want 10", cfg.RateLimit)
	}
	if cfg.RateWindow != 30 {
		t.Fatalf("RateWindow = %d, want 30", cfg.RateWindow)
	}
}

func TestChannelGateEnabled(t *testing.T) {
	if ChannelGateEnabled(ChannelConfig{}) {
		t.Fatal("empty config should not enable channel gate")
	}
	if !ChannelGateEnabled(ChannelConfig{MaxMessageSize: 512}) {
		t.Fatal("max message size alone should enable channel gate")
	}
	if !ChannelGateEnabled(ChannelConfig{RateLimit: 1}) {
		t.Fatal("rate limit alone should enable channel gate")
	}
	if ChannelGateEnabled(ChannelConfig{MaxMessageSize: 0, RateLimit: 0}) {
		t.Fatal("both zero should disable channel gate")
	}
}

func TestNormalizedRateWindow(t *testing.T) {
	if got := NormalizedRateWindow(ChannelConfig{RateLimit: 0, RateWindow: 0}); got != 0 {
		t.Fatalf("unlimited limit: got %d, want 0", got)
	}
	if got := NormalizedRateWindow(ChannelConfig{RateLimit: 5, RateWindow: 30}); got != 30 {
		t.Fatalf("positive window: got %d, want 30", got)
	}
	if got := NormalizedRateWindow(ChannelConfig{RateLimit: 5, RateWindow: 0}); got != DefaultChannelRateWindow {
		t.Fatalf("zero window with limit: got %d, want %d", got, DefaultChannelRateWindow)
	}
}
