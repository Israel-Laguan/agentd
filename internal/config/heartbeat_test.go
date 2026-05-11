package config

import (
	"testing"
	"time"
)

func TestHeartbeatConfig_Defaults(t *testing.T) {
	cfg := HeartbeatConfig{
		StaleAfter: defaultHeartbeatStaleAfter,
	}
	if cfg.StaleAfter != 2*time.Minute {
		t.Errorf("StaleAfter = %v, want 2m", cfg.StaleAfter)
	}
}

func TestHeartbeatConfig_Custom(t *testing.T) {
	cfg := HeartbeatConfig{
		StaleAfter: 5 * time.Minute,
	}
	if cfg.StaleAfter != 5*time.Minute {
		t.Errorf("StaleAfter = %v, want 5m", cfg.StaleAfter)
	}
}