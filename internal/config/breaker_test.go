package config

import (
	"testing"
	"time"
)

func TestBreakerConfig_Defaults(t *testing.T) {
	cfg := BreakerConfig{
		HandoffAfter: defaultBreakerHandoffAfter,
	}
	if cfg.HandoffAfter != 2*time.Minute {
		t.Errorf("HandoffAfter = %v, want 2m", cfg.HandoffAfter)
	}
}

func TestBreakerConfig_Custom(t *testing.T) {
	cfg := BreakerConfig{
		HandoffAfter: 5 * time.Minute,
	}
	if cfg.HandoffAfter != 5*time.Minute {
		t.Errorf("HandoffAfter = %v, want 5m", cfg.HandoffAfter)
	}
}